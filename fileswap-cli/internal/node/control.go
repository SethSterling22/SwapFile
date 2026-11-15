package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// The control socket lets other fileswap commands use the running `listen`
// instead of opening a second connection (the server allows one connection
// per identity and would disconnect `listen`). It is a Unix socket in a
// directory only this user can open; nothing is reachable from the network.
//
// Protocol: one JSON request line, one JSON response line.

// Request is a control command.
type Request struct {
	Cmd string `json:"cmd"` // status | sync | unpair | stop
	Arg string `json:"arg,omitempty"`
}

// Response answers a Request. Error and Detail carry an Error's code.
type Response struct {
	OK     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	Detail string  `json:"detail,omitempty"`
	Status *Status `json:"status,omitempty"`
	Name   string  `json:"name,omitempty"`
	ID     string  `json:"id,omitempty"`
}

// ErrNotRunning means no `listen` is running for this identity.
var ErrNotRunning = errors.New("no listen running")

// SocketPath is the control socket for a device code:
// $XDG_RUNTIME_DIR/fileswap/<code>.sock, else <tmp>/fileswap-<uid>/<code>.sock.
// Unix socket paths are limited to ~108 bytes, so overly long directories
// fall back to /tmp/fileswap-<uid>.
func SocketPath(code string) string {
	name := code + ".sock"
	for _, dir := range []string{os.Getenv("XDG_RUNTIME_DIR"), os.TempDir(), "/tmp"} {
		if dir == "" {
			continue
		}
		sub := "fileswap"
		if dir != os.Getenv("XDG_RUNTIME_DIR") {
			sub = fmt.Sprintf("fileswap-%d", os.Getuid())
		}
		if p := filepath.Join(dir, sub, name); len(p) <= maxSocketPath {
			return p
		}
	}
	return filepath.Join("/tmp", fmt.Sprintf("fileswap-%d", os.Getuid()), name)
}

const maxSocketPath = 100

// ListenControl creates this node's control socket. It fails with an
// ALREADY_RUNNING error if another listen owns it.
func (n *Node) ListenControl(ctx context.Context) (net.Listener, error) {
	path := SocketPath(n.ID())
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if resp, err := Call(ctx, path, Request{Cmd: "status"}); err == nil {
		pid := ""
		if resp.Status != nil {
			pid = fmt.Sprint(resp.Status.PID)
		}
		return nil, errf(CodeAlreadyRunning, pid)
	}
	_ = os.Remove(path) // stale socket from a process that did not exit cleanly
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return ln, nil
}

// ServeControl answers requests on ln until ctx ends, then removes the
// socket. stop is called for the "stop" command.
func (n *Node) ServeControl(ctx context.Context, ln net.Listener, stop func()) {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	defer os.Remove(ln.Addr().String())
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go n.serveConn(ctx, conn, stop)
	}
}

func (n *Node) serveConn(ctx context.Context, conn net.Conn, stop func()) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	var req Request
	if json.Unmarshal(line, &req) != nil {
		return
	}

	resp := Response{OK: true}
	switch req.Cmd {
	case "status":
		st := n.Status()
		resp.Status = &st
	case "sync":
		sctx, cancel := context.WithTimeout(ctx, SyncTimeout)
		name, err := n.Sync(sctx, req.Arg)
		cancel()
		resp.Name, resp.ID = name, req.Arg
		setErr(&resp, err)
	case "unpair":
		p, err := n.Unpair(ctx, req.Arg)
		resp.Name, resp.ID = p.Name, p.ID
		setErr(&resp, err)
	case "stop":
		defer stop()
	default:
		resp = Response{Error: "UNKNOWN_COMMAND", Detail: req.Cmd}
	}
	b, _ := json.Marshal(resp)
	_, _ = conn.Write(append(b, '\n'))
}

func setErr(r *Response, err error) {
	if err == nil {
		return
	}
	r.OK = false
	var e *Error
	if errors.As(err, &e) {
		r.Error, r.Detail = e.Code, e.Detail
	} else {
		r.Error, r.Detail = "INTERNAL", err.Error()
	}
}

// SyncTimeout bounds how long a pairing request waits for an answer.
const SyncTimeout = 2 * time.Minute

// Call sends one request to the listen owning the socket at path. It
// returns ErrNotRunning when nothing is listening there.
func Call(ctx context.Context, path string, req Request) (*Response, error) {
	var d net.Dialer
	dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	conn, err := d.DialContext(dctx, "unix", path)
	cancel()
	if err != nil {
		return nil, ErrNotRunning
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	b, _ := json.Marshal(req)
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("control socket: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AsError turns a failed Response back into an *Error.
func (r *Response) AsError() error {
	if r.OK {
		return nil
	}
	return &Error{Code: r.Error, Detail: r.Detail}
}
