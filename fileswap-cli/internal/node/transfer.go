package node

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/identity"
	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/peer"
	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/signaling"
	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/transfer"
)

// Transfer error codes, on top of the transfer package's.
const (
	CodeNotFile        = "NOT_A_FILE"      // tfr path is a directory or special file
	CodePeerAborted    = "PEER_ABORTED"    // the other side cancelled; Detail has its reason
	CodeConnectionLost = "CONNECTION_LOST" // the WebRTC connection failed or closed
	CodeConnectTimeout = "CONNECT_TIMEOUT" // the DataChannel never opened
)

// Abort reasons sent to the other side (docs/protocol.md §3).
const (
	abortCancelled    = "CANCELLED"
	abortBusy         = "BUSY"
	abortNotReceiving = "NOT_RECEIVING"
	abortError        = "ERROR"
)

const (
	openTimeout       = 30 * time.Second
	maxIncoming       = 4
	presenceGrace     = 2 * time.Second
	lingerAfterResult = 2 * time.Second
	connLostAfterDrop = 10 * time.Second
)

// xfer is one transfer in progress, in either direction.
type xfer struct {
	conn     *peer.Conn
	peerID   string
	incoming bool
	once     sync.Once
	aborted  chan string // reason from the other side, or a local failure code
}

func (x *xfer) stop(reason string) {
	x.once.Do(func() { x.aborted <- reason })
}

func (n *Node) signalSender(to string) func(peer.Signal) error {
	return func(s peer.Signal) error {
		return n.client.Send(context.Background(), map[string]any{"type": "signal", "to": to, "data": s})
	}
}

// onSignal routes WebRTC signals from paired devices to their transfer.
func (n *Node) onSignal(m signaling.Msg) {
	from := m.FromID()
	if !n.identity().HasPair(from) {
		return // the server already filters this; defense in depth
	}
	s, err := peer.ParseSignal(m.Data)
	if err != nil || s.TID == "" {
		return
	}
	n.mu.Lock()
	x := n.transfers[s.TID]
	n.mu.Unlock()
	if x != nil && x.peerID != from {
		return
	}

	switch {
	case s.Type == "abort":
		if x != nil {
			x.stop(CodePeerAborted + ":" + s.Reason)
		}
	case s.Type == "offer" && x == nil:
		n.acceptOffer(from, s)
	case x != nil:
		if err := x.conn.Handle(s); err != nil {
			x.stop(CodeConnectionLost)
		}
	}
}

func (n *Node) track(tid string, x *xfer) {
	n.mu.Lock()
	n.transfers[tid] = x
	n.mu.Unlock()
}

func (n *Node) untrack(tid string) {
	n.mu.Lock()
	delete(n.transfers, tid)
	n.mu.Unlock()
}

func (n *Node) incomingCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	c := 0
	for _, x := range n.transfers {
		if x.incoming {
			c++
		}
	}
	return c
}

// ─── Receiving ────────────────────────────────────────────────────────────

func (n *Node) acceptOffer(from string, s peer.Signal) {
	send := n.signalSender(from)
	reject := func(reason string) { _ = send(peer.Signal{Type: "abort", TID: s.TID, Reason: reason}) }
	dir := n.opts.InboxDir
	switch {
	case dir == "":
		reject(abortNotReceiving)
		return
	case n.incomingCount() >= maxIncoming:
		reject(abortBusy)
		n.opts.Log("receiveBusy", n.peerName(from))
		return
	}

	conn, err := peer.New(s.TID, send)
	if err != nil {
		reject(abortError)
		return
	}
	x := &xfer{conn: conn, peerID: from, incoming: true, aborted: make(chan string, 1)}
	n.track(s.TID, x)

	opened := make(chan *transfer.Receiver, 1)
	conn.PC.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != transfer.ChannelLabel {
			return
		}
		r := transfer.NewReceiver(dir, dc)
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			if r.Meta() == nil && msg.IsString {
				defer func() {
					if m := r.Meta(); m != nil {
						n.opts.Log("receiving", m.Name, fmtBytes(m.Size), n.peerName(from))
					}
				}()
			}
			r.HandleMessage(msg.IsString, msg.Data)
		})
		dc.OnClose(func() { x.stop(CodeConnectionLost) })
		opened <- r
	})
	watchConnection(conn, x)

	if err := conn.Handle(s); err != nil {
		n.untrack(s.TID)
		conn.Abort(abortError)
		return
	}
	go n.finishReceive(s.TID, x, opened)
}

func (n *Node) finishReceive(tid string, x *xfer, opened chan *transfer.Receiver) {
	defer n.untrack(tid)
	from := n.peerName(x.peerID)
	var r *transfer.Receiver
	select {
	case r = <-opened:
	case reason := <-x.aborted:
		x.conn.Close()
		n.opts.Log("receiveFailed", from, reasonText(reason))
		return
	case <-time.After(openTimeout):
		x.conn.Abort(abortError)
		n.opts.Log("receiveFailed", from, CodeConnectTimeout)
		return
	}

	var res transfer.Result
	select {
	case res = <-r.Done():
	case reason := <-x.aborted:
		r.Abort(reason)
		res = <-r.Done()
	}
	if res.Err != nil {
		if res.Meta.Name != "" {
			n.opts.Log("receiveFailedFile", res.Meta.Name, from, reasonText(res.Err.Error()))
		} else {
			n.opts.Log("receiveFailed", from, reasonText(res.Err.Error()))
		}
		x.conn.Close()
		return
	}
	n.opts.Log("received", res.Path, fmtBytes(res.Meta.Size), from, res.SHA256[:12])
	// Give the ack time to leave before closing; the sender closes first normally.
	time.Sleep(lingerAfterResult)
	x.conn.Close()
}

// watchConnection reports a failed or long-disconnected connection.
func watchConnection(conn *peer.Conn, x *xfer) {
	var drop *time.Timer
	conn.PC.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		switch st {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			x.stop(CodeConnectionLost)
		case webrtc.PeerConnectionStateDisconnected:
			drop = time.AfterFunc(connLostAfterDrop, func() { x.stop(CodeConnectionLost) })
		case webrtc.PeerConnectionStateConnected:
			if drop != nil {
				drop.Stop()
			}
		}
	})
}

// ─── Sending ──────────────────────────────────────────────────────────────

// SendResult describes a completed send.
type SendResult struct {
	Peer   identity.Pair `json:"peer"`
	Name   string        `json:"name"`
	Size   int64         `json:"size"`
	SHA256 string        `json:"sha256"`
}

// Send transfers the file at path to a paired device (code, prefix or name).
func (n *Node) Send(ctx context.Context, ref, path string, progress transfer.ProgressFunc) (*SendResult, error) {
	id := n.reload()
	p, err := id.Find(ref)
	if err != nil {
		return nil, errf(CodeAmbiguous, ref)
	}
	if p == nil {
		return nil, errf(CodeNotFound, ref)
	}
	pair := *p

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errf(CodeNotFile, path)
	}

	if err := n.WaitOnline(ctx); err != nil {
		return nil, err
	}
	if !n.waitPeerOnline(ctx, pair.ID) {
		return nil, errf("DEVICE_OFFLINE", pair.Name)
	}

	tid := peer.NewTID()
	conn, err := peer.New(tid, n.signalSender(pair.ID))
	if err != nil {
		return nil, err
	}
	x := &xfer{conn: conn, peerID: pair.ID, aborted: make(chan string, 1)}
	n.track(tid, x)
	defer n.untrack(tid)
	defer conn.Close()

	ordered := true
	dc, err := conn.PC.CreateDataChannel(transfer.ChannelLabel, &webrtc.DataChannelInit{Ordered: &ordered})
	if err != nil {
		return nil, err
	}
	sender := transfer.NewSender(dc)
	dc.SetBufferedAmountLowThreshold(transfer.LowThreshold)
	dc.OnBufferedAmountLow(sender.OnBufferedLow)
	dc.OnMessage(func(msg webrtc.DataChannelMessage) { sender.OnMessage(msg.IsString, msg.Data) })
	open := make(chan struct{})
	dc.OnOpen(func() { close(open) })
	watchConnection(conn, x)

	if err := conn.Offer(); err != nil {
		return nil, errf(CodeOffline, err.Error())
	}
	n.opts.Log("connecting", pair.Name)
	select {
	case <-open:
	case reason := <-x.aborted:
		return nil, reasonErr(reason)
	case <-ctx.Done():
		conn.Abort(abortCancelled)
		return nil, ctx.Err()
	case <-time.After(openTimeout):
		conn.Abort(abortError)
		return nil, errf(CodeConnectTimeout, pair.Name)
	}

	// Fail the send as soon as the other side aborts or the connection drops.
	sctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go func() {
		select {
		case reason := <-x.aborted:
			cancel(reasonErr(reason))
		case <-sctx.Done():
		}
	}()

	name := filepath.Base(path)
	n.opts.Log("sending", name, fmtBytes(st.Size()), pair.Name)
	sum, err := sender.Send(sctx, f, transfer.Meta{Name: name, Size: st.Size(), Type: mimeType(name)}, progress)
	if err != nil {
		if cause := context.Cause(sctx); cause != nil && !errors.Is(cause, context.Canceled) {
			err = cause
		}
		if ctx.Err() != nil {
			conn.Abort(abortCancelled)
			n.opts.Log("sendCancelled", name, pair.Name)
			return nil, ctx.Err()
		}
		n.opts.Log("sendFailed", name, pair.Name, reasonText(err.Error()))
		var te *transfer.Error
		if errors.As(err, &te) {
			if te.Code == transfer.ErrStalled || te.Code == transfer.ErrSizeMismatch {
				conn.Abort(abortError) // the receiver may still be waiting
			}
			return nil, errf(te.Code, te.Detail)
		}
		var ne *Error
		if !errors.As(err, &ne) {
			conn.Abort(abortError) // local failure, e.g. a read error
		}
		return nil, err
	}
	n.opts.Log("sent", name, fmtBytes(st.Size()), pair.Name, sum[:12])
	return &SendResult{Peer: pair, Name: name, Size: st.Size(), SHA256: sum}, nil
}

// waitPeerOnline waits briefly for the presence message of a peer that
// connected around the same time as us.
func (n *Node) waitPeerOnline(ctx context.Context, code string) bool {
	deadline := time.Now().Add(presenceGrace)
	for {
		n.mu.Lock()
		online := n.presence[code]
		n.mu.Unlock()
		if online || time.Now().After(deadline) || ctx.Err() != nil {
			return online
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func mimeType(name string) string {
	if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// reasonErr turns an xfer stop reason into an error.
func reasonErr(reason string) error {
	if code, detail, ok := cutPeerAbort(reason); ok {
		return errf(code, detail)
	}
	return errf(reason, "")
}

func cutPeerAbort(reason string) (code, detail string, ok bool) {
	const p = CodePeerAborted + ":"
	if len(reason) > len(p) && reason[:len(p)] == p {
		return CodePeerAborted, reason[len(p):], true
	}
	if reason == p {
		return CodePeerAborted, "", true
	}
	return "", "", false
}

// reasonText is the reason passed to log events: an error code, optionally
// "CODE: detail" or "PEER_ABORTED:<reason>". The CLI translates it.
func reasonText(reason string) string { return reason }

func fmtBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
