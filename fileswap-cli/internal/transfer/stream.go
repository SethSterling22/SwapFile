package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Channel is the part of a WebRTC DataChannel the transfer code uses.
// *webrtc.DataChannel satisfies it.
type Channel interface {
	SendText(string) error
	Send([]byte) error
	BufferedAmount() uint64
}

const (
	// maxBuffered pauses the sender while this much is queued in the channel.
	maxBuffered = 1 << 20
	// LowThreshold is the buffered amount at which the sender resumes; set it
	// with SetBufferedAmountLowThreshold and call Sender.OnBufferedLow.
	LowThreshold = 256 << 10
	// StallTimeout fails a transfer that makes no progress for this long.
	StallTimeout = 30 * time.Second
	// AckTimeout bounds the wait for the receiver's ack after the end frame.
	AckTimeout = 60 * time.Second
)

// Transfer error codes (beyond the Ack ones in frames.go).
const (
	ErrNoSpace = "NO_SPACE"
	// ErrTooLarge comes only from browsers that cannot write to disk while receiving.
	ErrTooLarge = "TOO_LARGE"
	ErrStalled  = "STALLED"
	ErrNoAck    = "NO_ACK"
)

// Error is a transfer failure with a protocol code.
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

// ProgressFunc reports bytes done out of total.
type ProgressFunc func(done, total int64)

// ─── Sending ──────────────────────────────────────────────────────────────

// Sender streams one file over a channel. Wire the channel's callbacks to
// OnMessage and OnBufferedLow before calling Send.
type Sender struct {
	ch  Channel
	low chan struct{}
	ack chan Ack
}

// NewSender returns a Sender for ch.
func NewSender(ch Channel) *Sender {
	return &Sender{ch: ch, low: make(chan struct{}, 1), ack: make(chan Ack, 1)}
}

// OnBufferedLow must be called when the channel's buffered amount drops
// below LowThreshold.
func (s *Sender) OnBufferedLow() {
	select {
	case s.low <- struct{}{}:
	default:
	}
}

// OnMessage must receive every message from the channel (the receiver's ack).
func (s *Sender) OnMessage(isString bool, data []byte) {
	if !isString {
		return
	}
	var a Ack
	if json.Unmarshal(data, &a) == nil && a.Ack {
		select {
		case s.ack <- a:
		default:
		}
	}
}

// Send writes meta, the content of r in chunks, and the end frame with the
// SHA-256, then waits for the ack. It returns the hex SHA-256 on success.
// An early ack (e.g. NO_SPACE right after meta) aborts the send.
func (s *Sender) Send(ctx context.Context, r io.Reader, meta Meta, progress ProgressFunc) (string, error) {
	meta.V = ProtocolVersion
	if meta.Type == "" {
		meta.Type = "application/octet-stream"
	}
	if err := s.sendJSON(meta); err != nil {
		return "", err
	}
	h := sha256.New()
	buf := make([]byte, ChunkSize)
	var sent int64
	for {
		if err := s.wait(ctx); err != nil {
			return "", err
		}
		n, rerr := io.ReadFull(r, buf)
		if n > 0 {
			h.Write(buf[:n])
			if err := s.ch.Send(buf[:n]); err != nil {
				return "", err
			}
			sent += int64(n)
			if progress != nil {
				progress(sent, meta.Size)
			}
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	if meta.Size != UnknownSize && sent != meta.Size {
		return "", &Error{Code: ErrSizeMismatch, Detail: "file changed while sending"}
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if err := s.sendJSON(End{End: true, Size: sent, SHA256: sum}); err != nil {
		return "", err
	}

	select {
	case a := <-s.ack:
		return sum, checkAck(a, sum)
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(AckTimeout):
		return "", &Error{Code: ErrNoAck}
	}
}

func checkAck(a Ack, sum string) error {
	switch {
	case !a.OK:
		return &Error{Code: orDefault(a.Error, ErrWriteFailed)}
	case a.SHA256 != "" && a.SHA256 != sum:
		return &Error{Code: ErrHashMismatch}
	}
	return nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// wait applies backpressure and notices an early ack from the receiver.
func (s *Sender) wait(ctx context.Context) error {
	for s.ch.BufferedAmount() > maxBuffered {
		select {
		case <-s.low:
		case a := <-s.ack:
			return &Error{Code: orDefault(a.Error, ErrWriteFailed)}
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(StallTimeout):
			return &Error{Code: ErrStalled}
		}
	}
	select {
	case a := <-s.ack:
		return &Error{Code: orDefault(a.Error, ErrWriteFailed)}
	default:
		return nil
	}
}

func (s *Sender) sendJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.ch.SendText(string(b))
}

// ─── Receiving ────────────────────────────────────────────────────────────

// Result is the outcome of a received transfer.
type Result struct {
	Meta   Meta
	Path   string // final file path on success
	SHA256 string
	Err    error
}

// Receiver writes an incoming transfer into Dir. Feed it every channel
// message with HandleMessage; the result arrives on Done.
type Receiver struct {
	Dir      string
	Ch       Channel
	Progress ProgressFunc

	mu       sync.Mutex
	meta     *Meta
	tmp      *os.File
	h        hash.Hash
	received int64
	finished bool
	done     chan Result
}

// NewReceiver returns a Receiver writing into dir and acking over ch.
func NewReceiver(dir string, ch Channel) *Receiver {
	return &Receiver{Dir: dir, Ch: ch, done: make(chan Result, 1)}
}

// Done delivers the result once.
func (r *Receiver) Done() <-chan Result { return r.done }

// Meta returns the announced file, once the meta frame arrived.
func (r *Receiver) Meta() *Meta {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.meta
}

// HandleMessage processes one channel message.
func (r *Receiver) HandleMessage(isString bool, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	if !isString {
		r.chunk(data)
		return
	}
	var probe struct {
		End bool `json:"__end"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return
	}
	if probe.End {
		var end End
		_ = json.Unmarshal(data, &end)
		r.end(end)
		return
	}
	if r.meta == nil {
		var m Meta
		if json.Unmarshal(data, &m) == nil {
			r.start(m)
		}
	}
}

func (r *Receiver) start(m Meta) {
	if m.Size < UnknownSize || (m.Size == UnknownSize && m.Approx < 0) {
		return
	}
	r.meta = &m
	need := m.Size
	if need == UnknownSize {
		need = m.Approx + m.Approx/100 + 64<<10 // a ZIP is at most a little bigger than its files
	}
	if free, err := freeBytes(r.Dir); err == nil && uint64(need) > free {
		r.fail(ErrNoSpace, fmt.Sprintf("%d bytes needed, %d free", need, free))
		return
	}
	tmp, err := os.CreateTemp(r.Dir, ".fileswap-*.part")
	if err != nil {
		r.fail(ErrWriteFailed, err.Error())
		return
	}
	r.tmp, r.h = tmp, sha256.New()
}

func (r *Receiver) chunk(b []byte) {
	if r.meta == nil || r.tmp == nil {
		return
	}
	if r.meta.Size != UnknownSize && r.received+int64(len(b)) > r.meta.Size {
		r.fail(ErrSizeMismatch, "more data than announced")
		return
	}
	if _, err := r.tmp.Write(b); err != nil {
		r.fail(ErrWriteFailed, err.Error())
		return
	}
	r.h.Write(b)
	r.received += int64(len(b))
	if r.Progress != nil {
		r.Progress(r.received, r.meta.Size)
	}
}

func (r *Receiver) end(end End) {
	if r.meta == nil || r.tmp == nil {
		return
	}
	want := r.meta.Size
	if want == UnknownSize {
		want = end.Size
	}
	if r.received != want || (end.Size != 0 && end.Size != r.received) {
		r.fail(ErrSizeMismatch, fmt.Sprintf("got %d of %d bytes", r.received, want))
		return
	}
	sum := hex.EncodeToString(r.h.Sum(nil))
	if end.SHA256 != "" && end.SHA256 != sum {
		r.fail(ErrHashMismatch, "")
		return
	}
	if err := r.tmp.Sync(); err != nil {
		r.fail(ErrWriteFailed, err.Error())
		return
	}
	if err := r.tmp.Close(); err != nil {
		r.fail(ErrWriteFailed, err.Error())
		return
	}
	// Reserve the final name without overwriting anything, then move the data in.
	final, err := CreateUnique(r.Dir, SafeName(r.meta.Name))
	if err != nil {
		r.fail(ErrWriteFailed, err.Error())
		return
	}
	final.Close()
	if err := os.Rename(r.tmp.Name(), final.Name()); err != nil {
		os.Remove(final.Name())
		r.fail(ErrWriteFailed, err.Error())
		return
	}
	_ = os.Chmod(final.Name(), 0o644)
	r.tmp = nil
	r.finished = true
	r.sendAck(Ack{Ack: true, OK: true, Received: r.received, SHA256: sum})
	meta := *r.meta
	meta.Size = r.received
	r.done <- Result{Meta: meta, Path: final.Name(), SHA256: sum}
}

// Abort discards a transfer that will not complete (connection lost,
// cancelled by the sender). It is a no-op once finished.
func (r *Receiver) Abort(code string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.cleanup()
	r.finished = true
	meta := Meta{}
	if r.meta != nil {
		meta = *r.meta
	}
	r.done <- Result{Meta: meta, Err: &Error{Code: code}}
}

func (r *Receiver) fail(code, detail string) {
	r.cleanup()
	r.finished = true
	r.sendAck(Ack{Ack: true, OK: false, Received: r.received, Error: code})
	meta := Meta{}
	if r.meta != nil {
		meta = *r.meta
	}
	r.done <- Result{Meta: meta, Err: &Error{Code: code, Detail: detail}}
}

func (r *Receiver) cleanup() {
	if r.tmp != nil {
		r.tmp.Close()
		os.Remove(r.tmp.Name())
		r.tmp = nil
	}
}

func (r *Receiver) sendAck(a Ack) {
	if b, err := json.Marshal(a); err == nil && r.Ch != nil {
		_ = r.Ch.SendText(string(b))
	}
}

// freeBytes is the space available to this user on dir's filesystem.
func freeBytes(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// IsCode reports whether err is a transfer *Error with that code.
func IsCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// CleanStale removes .part files left by transfers interrupted by a crash.
func CleanStale(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, ".fileswap-*.part"))
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil && time.Since(st.ModTime()) > time.Hour {
			os.Remove(m)
		}
	}
}
