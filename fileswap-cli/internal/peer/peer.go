// Package peer wraps a pion WebRTC PeerConnection for one FileSwap
// transfer (docs/protocol.md §3): offer/answer and trickled ICE candidates
// carried in signal messages, tagged with the transfer id.
package peer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/pion/webrtc/v4"
)

// ICEServers matches the web client.
var ICEServers = []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302", "stun:stun1.l.google.com:19302"}}}

const maxQueuedCandidates = 64

// Signal is the `data` of a signal message.
type Signal struct {
	Type      string                   `json:"type"` // offer | answer | candidate | abort
	SDP       string                   `json:"sdp,omitempty"`
	TID       string                   `json:"tid,omitempty"`
	Candidate *webrtc.ICECandidateInit `json:"candidate,omitempty"`
	Reason    string                   `json:"reason,omitempty"` // abort only
	// Kind is set on the offer of a non-file connection: "clip" browses or
	// requests from the other device's clipboard.
	Kind string `json:"kind,omitempty"`
}

// ParseSignal decodes signal data.
func ParseSignal(raw json.RawMessage) (Signal, error) {
	var s Signal
	err := json.Unmarshal(raw, &s)
	return s, err
}

// NewTID returns a random transfer id (32 hex characters).
func NewTID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Conn is the PeerConnection of one transfer.
type Conn struct {
	PC   *webrtc.PeerConnection
	TID  string
	Kind string // sent with the offer, see Signal.Kind
	send func(Signal) error

	mu        sync.Mutex
	remoteSet bool
	queued    []webrtc.ICECandidateInit
	closed    bool
}

// New creates a connection whose signals go through send.
func New(tid string, send func(Signal) error) (*Conn, error) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: ICEServers})
	if err != nil {
		return nil, err
	}
	c := &Conn{PC: pc, TID: tid, send: send}
	pc.OnICECandidate(func(cand *webrtc.ICECandidate) {
		if cand == nil {
			return
		}
		init := cand.ToJSON()
		_ = send(Signal{Type: "candidate", TID: tid, Candidate: &init})
	})
	return c, nil
}

// Offer creates and sends the offer (sender side). Create the DataChannel first.
func (c *Conn) Offer() error {
	offer, err := c.PC.CreateOffer(nil)
	if err != nil {
		return err
	}
	if err := c.PC.SetLocalDescription(offer); err != nil {
		return err
	}
	return c.send(Signal{Type: "offer", SDP: offer.SDP, TID: c.TID, Kind: c.Kind})
}

// Handle applies an offer, answer or candidate from the other side.
func (c *Conn) Handle(s Signal) error {
	switch s.Type {
	case "offer":
		if err := c.setRemote(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: s.SDP}); err != nil {
			return err
		}
		answer, err := c.PC.CreateAnswer(nil)
		if err != nil {
			return err
		}
		if err := c.PC.SetLocalDescription(answer); err != nil {
			return err
		}
		return c.send(Signal{Type: "answer", SDP: answer.SDP, TID: c.TID})
	case "answer":
		c.mu.Lock()
		done := c.remoteSet
		c.mu.Unlock()
		if done {
			return nil
		}
		return c.setRemote(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: s.SDP})
	case "candidate":
		if s.Candidate == nil || s.Candidate.Candidate == "" {
			return nil
		}
		c.mu.Lock()
		if !c.remoteSet {
			// Candidates can arrive before the description they belong to
			if len(c.queued) < maxQueuedCandidates {
				c.queued = append(c.queued, *s.Candidate)
			}
			c.mu.Unlock()
			return nil
		}
		c.mu.Unlock()
		_ = c.PC.AddICECandidate(*s.Candidate) // a bad candidate must not end the transfer
	}
	return nil
}

func (c *Conn) setRemote(d webrtc.SessionDescription) error {
	if err := c.PC.SetRemoteDescription(d); err != nil {
		return err
	}
	c.mu.Lock()
	c.remoteSet = true
	queued := c.queued
	c.queued = nil
	c.mu.Unlock()
	for _, cand := range queued {
		_ = c.PC.AddICECandidate(cand)
	}
	return nil
}

// Abort tells the other side this transfer is over, then closes.
func (c *Conn) Abort(reason string) {
	_ = c.send(Signal{Type: "abort", TID: c.TID, Reason: reason})
	c.Close()
}

// Close closes the PeerConnection once.
func (c *Conn) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	_ = c.PC.Close()
}
