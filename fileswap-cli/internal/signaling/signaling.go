// Package signaling is the WebSocket client for the FileSwap signaling
// server (docs/protocol.md §2): register, keep-alive, reconnect, and
// delivery of server messages.
//
// Only outbound connections are made; nothing listens on a port.
package signaling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Close codes sent by the server.
const (
	CloseReplaced = 4001 // another connection registered with the same secret
	CloseExpired  = 4002 // idle session expired
)

var (
	// ErrReplaced is returned by Run when another connection took over this identity.
	ErrReplaced = errors.New("replaced by another connection with the same identity")
	// ErrOffline is returned by Send when there is no connection.
	ErrOffline = errors.New("not connected to the server")
)

// Msg is a server → client message. Fields are filled depending on Type.
type Msg struct {
	Type     string          `json:"type"`
	DeviceID string          `json:"deviceId,omitempty"`
	Name     string          `json:"name,omitempty"`
	Kind     string          `json:"kind,omitempty"`
	Online   bool            `json:"online,omitempty"`
	From     json.RawMessage `json:"from,omitempty"` // {id,name} in pair-*, a code in signal
	By       string          `json:"by,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Code     string          `json:"code,omitempty"`
	Message  string          `json:"message,omitempty"`
}

// Peer returns the sender of a pair-request / pair-accepted / pair-rejected.
func (m Msg) Peer() (id, name string) {
	var p struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if json.Unmarshal(m.From, &p) == nil {
		return p.ID, p.Name
	}
	return "", ""
}

// FromID returns the sender code of a signal message.
func (m Msg) FromID() string {
	var s string
	if json.Unmarshal(m.From, &s) == nil {
		return s
	}
	id, _ := m.Peer()
	return id
}

// Client keeps one registered connection to the server, reconnecting with
// backoff until its context is cancelled or the identity is taken over.
type Client struct {
	URL    string
	Secret string
	// Hello returns the name and paired codes sent with every register.
	Hello func() (name string, pairedIDs []string)
	// Kind is the device kind announced on register (laptop, server…); optional.
	Kind string
	// OnMessage receives every server message, on the read goroutine.
	OnMessage func(Msg)
	// OnState is told when the client becomes registered (online) or loses
	// the connection; err explains why it went offline.
	OnState func(online bool, err error)
	// PingInterval keeps the server's idle timer from expiring (default 5 min).
	PingInterval time.Duration
	// UserAgent is sent on the WebSocket handshake.
	UserAgent string

	mu   sync.Mutex
	conn *websocket.Conn
}

const (
	dialTimeout  = 10 * time.Second
	writeTimeout = 10 * time.Second
	maxBackoff   = 30 * time.Second
)

// Run connects and stays connected. It returns ctx.Err() when ctx ends, or
// ErrReplaced when the server closes the socket with 4001 (no reconnect).
func (c *Client) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		registered, err := c.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if c.OnState != nil {
			c.OnState(false, err)
		}
		if errors.Is(err, ErrReplaced) {
			return err
		}
		if registered {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (c *Client) session(ctx context.Context) (registered bool, err error) {
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	opts := &websocket.DialOptions{}
	if c.UserAgent != "" {
		opts.HTTPHeader = map[string][]string{"User-Agent": {c.UserAgent}}
	}
	conn, _, err := websocket.Dial(dctx, c.URL, opts)
	cancel()
	if err != nil {
		return false, err
	}
	conn.SetReadLimit(64 * 1024)
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
	}()

	sctx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		<-sctx.Done()
		conn.Close(websocket.StatusNormalClosure, "bye")
	}()
	go c.pinger(sctx)

	if err := c.Register(sctx); err != nil {
		return false, err
	}
	for {
		_, data, err := conn.Read(sctx)
		if err != nil {
			if websocket.CloseStatus(err) == CloseReplaced {
				return registered, ErrReplaced
			}
			return registered, err
		}
		var m Msg
		if json.Unmarshal(data, &m) != nil || m.Type == "" {
			continue
		}
		if m.Type == "registered" && !registered {
			registered = true
			if c.OnState != nil {
				c.OnState(true, nil)
			}
		}
		if c.OnMessage != nil {
			c.OnMessage(m)
		}
	}
}

func (c *Client) pinger(ctx context.Context) {
	every := c.PingInterval
	if every <= 0 {
		every = 5 * time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = c.Send(ctx, map[string]string{"type": "ping"})
		}
	}
}

// Register (re)sends the register message, e.g. after the pairs changed.
func (c *Client) Register(ctx context.Context) error {
	name, ids := "", []string{}
	if c.Hello != nil {
		name, ids = c.Hello()
	}
	msg := map[string]any{"type": "register", "secret": c.Secret, "name": name, "pairedIds": ids}
	if c.Kind != "" {
		msg["kind"] = c.Kind
	}
	return c.Send(ctx, msg)
}

// Send writes one JSON message. It is safe for concurrent use.
func (c *Client) Send(ctx context.Context, v any) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return ErrOffline
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if err := conn.Write(wctx, websocket.MessageText, b); err != nil {
		return fmt.Errorf("send %s: %w", typeOf(v), err)
	}
	return nil
}

func typeOf(v any) string {
	switch m := v.(type) {
	case map[string]any:
		if s, ok := m["type"].(string); ok {
			return s
		}
	case map[string]string:
		return m["type"]
	}
	return "message"
}
