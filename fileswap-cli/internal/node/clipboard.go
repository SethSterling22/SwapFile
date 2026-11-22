package node

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/clip"
	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/peer"
	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/transfer"
)

// Clipboard browsing and requests (docs/protocol.md §5). The viewer opens a
// WebRTC connection whose offer has kind "clip" and a DataChannel
// "fileswap-clip", and sends one request:
//
//	{"op":"list"}          → {"op":"list","items":[…],"used":…,"limit":…}
//	{"op":"get","id":"…"}  → {"op":"ok"} and the owner sends the attachment
//	                         as a normal transfer with meta.clip = id
//	                         (or {"op":"error","code":"NO_ITEM"})
//
// Nothing goes through the server but signaling.

// ClipChannel is the DataChannel label for clipboard requests.
const ClipChannel = "fileswap-clip"

// Clipboard error codes.
const (
	CodeNoClipboard = "NO_CLIPBOARD" // the device does not share a clipboard right now
	CodeNoItem      = "NO_ITEM"      // no such clipboard item, or it has no attachment
	CodeNoInbox     = "NOT_RECEIVING"
)

const (
	clipRequestTimeout = 20 * time.Second
	clipStartTimeout   = 45 * time.Second
	clipExpireEvery    = 10 * time.Minute
)

// RemoteFile is an attachment as listed to other devices.
type RemoteFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

// RemoteItem is a clipboard item as listed to other devices. Times are
// milliseconds since the epoch, as in JavaScript.
type RemoteItem struct {
	ID      string      `json:"id"`
	Text    string      `json:"text,omitempty"`
	File    *RemoteFile `json:"file,omitempty"`
	Created int64       `json:"created"`
	Expires int64       `json:"expires"`
}

// ClipList is a device's clipboard.
type ClipList struct {
	Op    string       `json:"op"`
	Items []RemoteItem `json:"items"`
	Used  int64        `json:"used"`
	Limit int64        `json:"limit"`
}

type clipRequest struct {
	Op string `json:"op"`
	ID string `json:"id,omitempty"`
}

type clipReply struct {
	Op   string `json:"op"`
	Code string `json:"code,omitempty"`
}

// ListClipboard returns this node's own clipboard as other devices see it.
func ListClipboard(s *clip.Store) (*ClipList, error) {
	entries, err := s.List()
	if err != nil {
		return nil, err
	}
	out := &ClipList{Op: "list", Items: []RemoteItem{}, Used: clip.Used(entries), Limit: s.Limit}
	for _, e := range entries {
		it := RemoteItem{ID: e.ID, Text: e.Text, Created: e.Created.UnixMilli(), Expires: e.Expires.UnixMilli()}
		if e.File != "" {
			it.File = &RemoteFile{Name: e.File, Size: e.Size, Type: e.Type}
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
}

// ─── Owner side ───────────────────────────────────────────────────────────

func (n *Node) acceptClip(from string, s peer.Signal) {
	send := n.signalSender(from)
	if n.opts.Clip == nil {
		_ = send(peer.Signal{Type: "abort", TID: s.TID, Reason: CodeNoClipboard})
		return
	}
	conn, err := peer.New(s.TID, send)
	if err != nil {
		_ = send(peer.Signal{Type: "abort", TID: s.TID, Reason: abortError})
		return
	}
	x := &xfer{conn: conn, peerID: from, aborted: make(chan string, 1)}
	n.track(s.TID, x)
	watchConnection(conn, x)
	conn.PC.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != ClipChannel {
			return
		}
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			var req clipRequest
			if !msg.IsString || json.Unmarshal(msg.Data, &req) != nil {
				return
			}
			n.serveClip(dc, from, req)
		})
	})
	if err := conn.Handle(s); err != nil {
		n.untrack(s.TID)
		conn.Abort(abortError)
		return
	}
	// The viewer closes the connection after the reply; this is a fallback.
	go func() {
		select {
		case <-x.aborted:
		case <-time.After(clipRequestTimeout):
		}
		n.untrack(s.TID)
		conn.Close()
	}()
}

func (n *Node) serveClip(dc *webrtc.DataChannel, from string, req clipRequest) {
	reply := func(v any) {
		if b, err := json.Marshal(v); err == nil {
			_ = dc.SendText(string(b))
		}
	}
	switch req.Op {
	case "list":
		list, err := ListClipboard(n.opts.Clip)
		if err != nil {
			reply(clipReply{Op: "error", Code: abortError})
			return
		}
		reply(list)
		n.opts.Log("clipListed", n.peerName(from), len(list.Items))
	case "get":
		e, err := n.opts.Clip.Get(req.ID)
		if err != nil || e.File == "" {
			reply(clipReply{Op: "error", Code: CodeNoItem})
			return
		}
		reply(clipReply{Op: "ok"})
		n.opts.Log("clipRequested", n.peerName(from), e.File)
		go func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, _ = n.send(ctx, from, []string{e.Path}, nil, e.ID)
		}()
	default:
		reply(clipReply{Op: "error", Code: "UNKNOWN_OP"})
	}
}

func (n *Node) expireClipboard(ctx context.Context) {
	t := time.NewTicker(clipExpireEvery)
	defer t.Stop()
	for {
		_, _ = n.opts.Clip.List() // removes expired items
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ─── Viewer side ──────────────────────────────────────────────────────────

// clipCall opens a clip connection to a paired, online device, sends req
// and returns the first reply.
func (n *Node) clipCall(ctx context.Context, ref string, req clipRequest) (json.RawMessage, string, error) {
	id := n.reload()
	p, err := id.Find(ref)
	if err != nil {
		return nil, "", errf(CodeAmbiguous, ref)
	}
	if p == nil {
		return nil, "", errf(CodeNotFound, ref)
	}
	pair := *p
	if err := n.WaitOnline(ctx); err != nil {
		return nil, pair.ID, err
	}
	if !n.waitPeerOnline(ctx, pair.ID) {
		return nil, pair.ID, errf("DEVICE_OFFLINE", pair.Name)
	}

	ctx, cancel := context.WithTimeout(ctx, clipRequestTimeout)
	defer cancel()
	tid := peer.NewTID()
	conn, err := peer.New(tid, n.signalSender(pair.ID))
	if err != nil {
		return nil, pair.ID, err
	}
	conn.Kind = "clip"
	x := &xfer{conn: conn, peerID: pair.ID, aborted: make(chan string, 1)}
	n.track(tid, x)
	defer n.untrack(tid)
	defer conn.Close()
	watchConnection(conn, x)

	ordered := true
	dc, err := conn.PC.CreateDataChannel(ClipChannel, &webrtc.DataChannelInit{Ordered: &ordered})
	if err != nil {
		return nil, pair.ID, err
	}
	replies := make(chan []byte, 1)
	dc.OnOpen(func() {
		b, _ := json.Marshal(req)
		_ = dc.SendText(string(b))
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if msg.IsString {
			select {
			case replies <- msg.Data:
			default:
			}
		}
	})
	if err := conn.Offer(); err != nil {
		return nil, pair.ID, errf(CodeOffline, err.Error())
	}
	select {
	case b := <-replies:
		var r clipReply
		if json.Unmarshal(b, &r) == nil && r.Op == "error" {
			return nil, pair.ID, errf(r.Code, pair.Name)
		}
		return b, pair.ID, nil
	case reason := <-x.aborted:
		if _, detail, ok := cutPeerAbort(reason); ok && detail == CodeNoClipboard {
			return nil, pair.ID, errf(CodeNoClipboard, pair.Name)
		}
		return nil, pair.ID, reasonErr(reason)
	case <-ctx.Done():
		return nil, pair.ID, errf(CodeConnectTimeout, pair.Name)
	}
}

// ClipboardOf lists the clipboard of a paired, online device.
func (n *Node) ClipboardOf(ctx context.Context, ref string) (*ClipList, string, error) {
	b, peerID, err := n.clipCall(ctx, ref, clipRequest{Op: "list"})
	if err != nil {
		return nil, peerID, err
	}
	var list ClipList
	if err := json.Unmarshal(b, &list); err != nil || list.Op != "list" {
		return nil, peerID, errf(abortError, "bad clipboard reply")
	}
	return &list, peerID, nil
}

type clipWait struct {
	started chan struct{}
	done    chan transfer.Result
	once    sync.Once
}

func clipKey(peerID, item string) string { return peerID + "/" + item }

// ClipGet asks a paired device for the attachment of one of its clipboard
// items and waits until it has been received into the swap folder.
func (n *Node) ClipGet(ctx context.Context, ref, item string) (*transfer.Result, error) {
	if n.opts.InboxDir == "" {
		return nil, errf(CodeNoInbox, "")
	}
	id := n.reload()
	p, err := id.Find(ref)
	if err != nil {
		return nil, errf(CodeAmbiguous, ref)
	}
	if p == nil {
		return nil, errf(CodeNotFound, ref)
	}
	w := &clipWait{started: make(chan struct{}), done: make(chan transfer.Result, 1)}
	key := clipKey(p.ID, item)
	n.mu.Lock()
	n.clipWaits[key] = w
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.clipWaits, key)
		n.mu.Unlock()
	}()

	if _, _, err := n.clipCall(ctx, ref, clipRequest{Op: "get", ID: item}); err != nil {
		return nil, err
	}
	select {
	case <-w.started:
	case res := <-w.done: // tiny files may finish before we notice the start
		return finishClip(res)
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(clipStartTimeout):
		return nil, errf(CodeConnectTimeout, p.Name)
	}
	select {
	case res := <-w.done:
		return finishClip(res)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func finishClip(res transfer.Result) (*transfer.Result, error) {
	if res.Err != nil {
		var te *transfer.Error
		if errors.As(res.Err, &te) {
			return nil, errf(te.Code, te.Detail)
		}
		return nil, res.Err
	}
	return &res, nil
}

func (n *Node) clipStarted(from, item string) {
	n.mu.Lock()
	w := n.clipWaits[clipKey(from, item)]
	n.mu.Unlock()
	if w != nil {
		w.once.Do(func() { close(w.started) })
	}
}

func (n *Node) clipDone(from string, res transfer.Result) {
	n.mu.Lock()
	w := n.clipWaits[clipKey(from, res.Meta.Clip)]
	n.mu.Unlock()
	if w != nil {
		select {
		case w.done <- res:
		default:
		}
	}
}
