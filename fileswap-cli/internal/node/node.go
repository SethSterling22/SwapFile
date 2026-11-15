// Package node is a FileSwap device running in this process: it stays
// registered with the signaling server, tracks which paired devices are
// online, answers pairing requests, and pairs or unpairs on request.
//
// The same Node backs one-shot commands (`sync`, `peers`, `unpair`) and the
// long-running `listen`, which also serves them over a local control socket.
package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/identity"
	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/signaling"
)

// Error codes returned by Node operations, also sent over the control socket.
const (
	CodeOffline        = "OFFLINE"         // not connected to the server
	CodeTimeout        = "TIMEOUT"         // no answer in time
	CodeRejected       = "REJECTED"        // the other device rejected the pairing
	CodeSelf           = "SELF"            // own code
	CodeAlreadyPaired  = "ALREADY_PAIRED"  // already paired with that device
	CodeNotFound       = "NOT_FOUND"       // no paired device matches
	CodeAmbiguous      = "AMBIGUOUS"       // several paired devices match
	CodeBusy           = "BUSY"            // another pairing request is pending
	CodeReplaced       = "REPLACED"        // another process took over this identity
	CodeAlreadyRunning = "ALREADY_RUNNING" // a listen is already running for this identity
)

// Error carries a code the CLI can translate, plus an optional detail
// (a device name, or the server's error code such as DEVICE_OFFLINE).
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return e.Code + ": " + e.Detail
	}
	return e.Code
}

func errf(code, detail string) error { return &Error{Code: code, Detail: detail} }

// Options configure a Node.
type Options struct {
	Dir     string // config directory holding identity.json
	Version string
	// HandleRequests answers incoming pairing requests. One-shot commands
	// leave it off so they never reject a request meant for `listen`.
	HandleRequests bool
	// Decide asks the user about a pairing request from a device that is not
	// in the allow list. nil rejects such requests.
	Decide func(ctx context.Context, code, name string) bool
	// Log receives human-readable events; nil discards them.
	Log func(event string, args ...any)
}

// Peer is a paired device and whether it is online now.
type Peer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
}

// Status describes a running node.
type Status struct {
	Online  bool      `json:"online"`
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Server  string    `json:"server"`
	PID     int       `json:"pid"`
	Since   time.Time `json:"since"`
	Version string    `json:"version"`
	Peers   []Peer    `json:"peers"`
}

// Node is one FileSwap device.
type Node struct {
	opts   Options
	client *signaling.Client
	since  time.Time

	mu       sync.Mutex
	id       *identity.Identity // last loaded copy; changes go through identity.Update
	online   bool
	presence map[string]bool
	onlineCh chan struct{} // closed while online
	pending  *pendingSync
	warned   bool // already logged that transfers are not supported

	syncMu sync.Mutex // one outgoing pairing request at a time
}

type pendingSync struct {
	code   string
	result chan syncResult
}

type syncResult struct {
	name string
	err  error
}

// New loads the identity from opts.Dir.
func New(opts Options) (*Node, error) {
	id, err := identity.Load(opts.Dir)
	if err != nil {
		return nil, err
	}
	if opts.Log == nil {
		opts.Log = func(string, ...any) {}
	}
	n := &Node{opts: opts, id: id, presence: map[string]bool{}, onlineCh: make(chan struct{}), since: time.Now()}
	n.client = &signaling.Client{
		URL:       id.Server,
		Secret:    id.Secret,
		Hello:     n.hello,
		OnMessage: n.handle,
		OnState:   n.setOnline,
		UserAgent: "fileswap/" + opts.Version,
	}
	return n, nil
}

// ID returns this node's device code.
func (n *Node) ID() string { return n.identity().ID() }

func (n *Node) identity() *identity.Identity {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.id
}

// update changes identity.json under its lock and refreshes the cached copy.
func (n *Node) update(fn func(*identity.Identity) error) error {
	id, err := identity.Update(n.opts.Dir, fn)
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.id = id
	n.mu.Unlock()
	return nil
}

// reload re-reads identity.json, picking up changes made by other commands.
func (n *Node) reload() *identity.Identity {
	if id, err := identity.Load(n.opts.Dir); err == nil {
		n.mu.Lock()
		n.id = id
		n.mu.Unlock()
	}
	return n.identity()
}

func (n *Node) hello() (string, []string) {
	id := n.identity()
	return id.Name, id.PairIDs()
}

// Run stays connected until ctx ends. It returns a CodeReplaced error if
// another process registers with the same identity.
func (n *Node) Run(ctx context.Context) error {
	err := n.client.Run(ctx)
	if errors.Is(err, signaling.ErrReplaced) {
		return errf(CodeReplaced, "")
	}
	return err
}

// WaitOnline blocks until the node is registered with the server.
func (n *Node) WaitOnline(ctx context.Context) error {
	for {
		n.mu.Lock()
		online, ch := n.online, n.onlineCh
		n.mu.Unlock()
		if online {
			return nil
		}
		select {
		case <-ctx.Done():
			return errf(CodeOffline, "")
		case <-ch:
		}
	}
}

func (n *Node) setOnline(online bool, err error) {
	n.mu.Lock()
	changed := n.online != online
	n.online = online
	if online {
		close(n.onlineCh)
	} else {
		if changed {
			n.onlineCh = make(chan struct{})
		}
		n.presence = map[string]bool{}
	}
	server := n.id.Server
	n.mu.Unlock()
	switch {
	case online:
		n.opts.Log("online", server)
	case changed:
		n.opts.Log("offline", errString(err))
	case err != nil:
		n.opts.Log("connectFailed", server, errString(err))
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (n *Node) handle(m signaling.Msg) {
	switch m.Type {
	case "presence":
		n.mu.Lock()
		was := n.presence[m.DeviceID]
		n.presence[m.DeviceID] = m.Online
		n.mu.Unlock()
		if m.Online && m.Name != "" {
			_ = n.update(func(id *identity.Identity) error {
				for _, p := range id.Pairs {
					if p.ID == m.DeviceID && p.Name != m.Name {
						id.SetPair(m.DeviceID, m.Name)
					}
				}
				return nil
			})
		}
		if was != m.Online {
			n.opts.Log(map[bool]string{true: "peerOnline", false: "peerOffline"}[m.Online], n.peerName(m.DeviceID))
		}

	case "pair-request":
		code, name := m.Peer()
		if identity.IsDeviceID(code) && n.opts.HandleRequests {
			go n.answerRequest(code, name)
		}

	case "pair-accepted":
		code, name := m.Peer()
		n.mu.Lock()
		p := n.pending
		n.mu.Unlock()
		if p == nil || p.code != code {
			return
		}
		err := n.update(func(id *identity.Identity) error { id.SetPair(code, name); return nil })
		if err == nil {
			_ = n.client.Register(context.Background())
		}
		p.result <- syncResult{name: name, err: err}

	case "pair-rejected":
		code, _ := m.Peer()
		n.mu.Lock()
		p := n.pending
		n.mu.Unlock()
		if p != nil && p.code == code {
			p.result <- syncResult{err: errf(CodeRejected, "")}
		}

	case "unpaired":
		name := n.peerName(m.By)
		removed := false
		_ = n.update(func(id *identity.Identity) error { removed = id.RemovePair(m.By); return nil })
		if removed {
			n.opts.Log("unpairedBy", name)
			_ = n.client.Register(context.Background())
		}

	case "error":
		n.mu.Lock()
		p := n.pending
		n.mu.Unlock()
		// The server does not say which request failed; while a pairing
		// request is pending these codes can only be about it.
		switch m.Code {
		case "DEVICE_OFFLINE", "TOO_MANY_REQUESTS", "RATE_LIMITED", "NOT_REGISTERED":
			if p != nil {
				p.result <- syncResult{err: errf(m.Code, "")}
				return
			}
		}
		n.opts.Log("serverError", m.Code, m.Message)

	case "signal":
		n.mu.Lock()
		warn := !n.warned
		n.warned = true
		n.mu.Unlock()
		if warn {
			n.opts.Log("noTransfers", n.peerName(m.FromID()))
		}
	}
}

func (n *Node) peerName(code string) string {
	for _, p := range n.identity().Pairs {
		if p.ID == code {
			return p.Name
		}
	}
	return code
}

// answerRequest accepts already-paired and allowed devices, asks Decide for
// the rest, and rejects when there is no one to ask.
func (n *Node) answerRequest(code, name string) {
	id := n.reload() // pick up `fileswap allow` run while we were online
	label := fmt.Sprintf("%s (%s)", name, code)
	var accept bool
	switch {
	case id.Allowed(code):
		accept = true
		n.opts.Log("autoAccept", label)
	case id.HasPair(code):
		accept = true
		n.opts.Log("autoAcceptPaired", label)
	case n.opts.Decide != nil:
		n.opts.Log("pairRequest", label)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		accept = n.opts.Decide(ctx, code, name)
		cancel()
	default:
		n.opts.Log("rejectNotAllowed", label, code)
	}

	ctx := context.Background()
	if !accept {
		_ = n.client.Send(ctx, map[string]string{"type": "pair-reject", "to": code})
		n.opts.Log("rejected", label)
		return
	}
	if err := n.client.Send(ctx, map[string]string{"type": "pair-accept", "to": code}); err != nil {
		n.opts.Log("acceptFailed", label, err.Error())
		return
	}
	if err := n.update(func(id *identity.Identity) error { id.SetPair(code, name); return nil }); err != nil {
		n.opts.Log("acceptFailed", label, err.Error())
		return
	}
	_ = n.client.Register(ctx)
	n.opts.Log("paired", label)
}

// Sync asks the device with that code to pair with this node and waits for
// the answer. It returns the other device's name.
func (n *Node) Sync(ctx context.Context, code string) (string, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	id := n.reload()
	switch {
	case code == id.ID():
		return "", errf(CodeSelf, "")
	case !identity.IsDeviceID(code):
		return "", errf(CodeNotFound, code)
	case id.HasPair(code):
		return "", errf(CodeAlreadyPaired, n.peerName(code))
	}
	if !n.syncMu.TryLock() {
		return "", errf(CodeBusy, "")
	}
	defer n.syncMu.Unlock()
	if err := n.WaitOnline(ctx); err != nil {
		return "", err
	}

	p := &pendingSync{code: code, result: make(chan syncResult, 4)}
	n.mu.Lock()
	n.pending = p
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		n.pending = nil
		n.mu.Unlock()
	}()

	if err := n.client.Send(ctx, map[string]string{"type": "pair-request", "to": code}); err != nil {
		return "", errf(CodeOffline, err.Error())
	}
	n.opts.Log("requestSent", code)
	select {
	case r := <-p.result:
		if r.err == nil {
			n.opts.Log("paired", fmt.Sprintf("%s (%s)", r.name, code))
		}
		return r.name, r.err
	case <-ctx.Done():
		return "", errf(CodeTimeout, "")
	}
}

// Unpair removes the pairing with a node reference (code, prefix or name) on
// both sides. When offline, only the local pairing is removed.
func (n *Node) Unpair(ctx context.Context, ref string) (identity.Pair, error) {
	id := n.reload()
	p, err := id.Find(ref)
	var amb *identity.ErrAmbiguous
	switch {
	case errors.As(err, &amb):
		return identity.Pair{}, errf(CodeAmbiguous, ref)
	case err != nil:
		return identity.Pair{}, err
	case p == nil:
		return identity.Pair{}, errf(CodeNotFound, ref)
	}
	pair := *p
	sendErr := n.client.Send(ctx, map[string]string{"type": "unpair", "with": pair.ID})
	if err := n.update(func(id *identity.Identity) error { id.RemovePair(pair.ID); return nil }); err != nil {
		return pair, err
	}
	_ = n.client.Register(ctx)
	n.mu.Lock()
	delete(n.presence, pair.ID)
	n.mu.Unlock()
	if sendErr != nil {
		return pair, errf(CodeOffline, pair.Name)
	}
	n.opts.Log("unpaired", fmt.Sprintf("%s (%s)", pair.Name, pair.ID))
	return pair, nil
}

// Status reports the node's connection and its paired devices.
func (n *Node) Status() Status {
	id := n.reload()
	n.mu.Lock()
	defer n.mu.Unlock()
	st := Status{Online: n.online, ID: id.ID(), Name: id.Name, Server: id.Server, PID: os.Getpid(), Since: n.since, Version: n.opts.Version}
	for _, p := range id.Pairs {
		st.Peers = append(st.Peers, Peer{ID: p.ID, Name: p.Name, Online: n.presence[p.ID]})
	}
	sort.SliceStable(st.Peers, func(i, j int) bool { return st.Peers[i].Online && !st.Peers[j].Online })
	return st
}
