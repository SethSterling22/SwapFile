package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pipe connects a Sender and a Receiver in memory. tamper, if set, can
// change each frame on its way to the receiver.
type pipe struct {
	recv   *Receiver
	send   *Sender
	tamper func(isString bool, b []byte) []byte
}

type toReceiver struct{ p *pipe }

func (c toReceiver) SendText(s string) error { c.p.deliver(true, []byte(s)); return nil }
func (c toReceiver) Send(b []byte) error     { c.p.deliver(false, append([]byte(nil), b...)); return nil }
func (c toReceiver) BufferedAmount() uint64  { return 0 }

type toSender struct{ p *pipe }

func (c toSender) SendText(s string) error { c.p.send.OnMessage(true, []byte(s)); return nil }
func (c toSender) Send([]byte) error       { return nil }
func (c toSender) BufferedAmount() uint64  { return 0 }

func (p *pipe) deliver(isString bool, b []byte) {
	if p.tamper != nil {
		if b = p.tamper(isString, b); b == nil {
			return
		}
	}
	p.recv.HandleMessage(isString, b)
}

func newPipe(dir string) *pipe {
	p := &pipe{}
	p.recv = NewReceiver(dir, toSender{p})
	p.send = NewSender(toReceiver{p})
	return p
}

func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + 7)
	}
	return b
}

func result(t *testing.T, r *Receiver) Result {
	t.Helper()
	select {
	case res := <-r.Done():
		return res
	case <-time.After(2 * time.Second):
		t.Fatal("no result")
		return Result{}
	}
}

func TestStreamRoundTrip(t *testing.T) {
	for _, size := range []int{0, 1, ChunkSize, 3*ChunkSize + 5} {
		dir := t.TempDir()
		p := newPipe(dir)
		data := payload(size)
		var last int64
		sum, err := p.send.Send(context.Background(), bytes.NewReader(data), Meta{Name: "../evil/report.bin", Size: int64(size)}, func(done, _ int64) { last = done })
		if err != nil {
			t.Fatalf("size %d: Send: %v", size, err)
		}
		res := result(t, p.recv)
		if res.Err != nil {
			t.Fatalf("size %d: receive: %v", size, res.Err)
		}
		want := sha256.Sum256(data)
		if sum != hex.EncodeToString(want[:]) || res.SHA256 != sum {
			t.Fatalf("size %d: hash mismatch", size)
		}
		if res.Path != filepath.Join(dir, "report.bin") {
			t.Fatalf("size %d: saved to %s", size, res.Path)
		}
		got, _ := os.ReadFile(res.Path)
		if !bytes.Equal(got, data) || last != int64(size) {
			t.Fatalf("size %d: content mismatch (progress %d)", size, last)
		}
		if parts, _ := filepath.Glob(filepath.Join(dir, ".fileswap-*.part")); len(parts) != 0 {
			t.Fatalf("leftover temp files: %v", parts)
		}
	}
}

func TestStreamNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("keep"), 0o644)
	p := newPipe(dir)
	if _, err := p.send.Send(context.Background(), bytes.NewReader([]byte("new")), Meta{Name: "a.txt", Size: 3}, nil); err != nil {
		t.Fatal(err)
	}
	res := result(t, p.recv)
	if filepath.Base(res.Path) != "a (1).txt" {
		t.Fatalf("saved as %s", res.Path)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "keep" {
		t.Fatal("existing file overwritten")
	}
}

func TestStreamHashMismatch(t *testing.T) {
	dir := t.TempDir()
	p := newPipe(dir)
	p.tamper = func(isString bool, b []byte) []byte {
		if !isString && len(b) > 0 {
			b[0] ^= 0xff // corrupt the first byte of every chunk
		}
		return b
	}
	_, err := p.send.Send(context.Background(), bytes.NewReader(payload(100)), Meta{Name: "x", Size: 100}, nil)
	if !IsCode(err, ErrHashMismatch) {
		t.Fatalf("Send err = %v, want HASH_MISMATCH", err)
	}
	if res := result(t, p.recv); !IsCode(res.Err, ErrHashMismatch) {
		t.Fatalf("receiver err = %v", res.Err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files left behind: %v", entries)
	}
}

func TestStreamSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	p := newPipe(dir)
	p.tamper = func(isString bool, b []byte) []byte {
		if isString {
			var m Meta
			if json.Unmarshal(b, &m) == nil && m.Name != "" {
				m.Size = 50 // announce less than what is sent
				b, _ = json.Marshal(m)
			}
		}
		return b
	}
	_, err := p.send.Send(context.Background(), bytes.NewReader(payload(100)), Meta{Name: "x", Size: 100}, nil)
	if !IsCode(err, ErrSizeMismatch) {
		t.Fatalf("Send err = %v, want SIZE_MISMATCH", err)
	}
}

func TestReceiverAbortCleansUp(t *testing.T) {
	dir := t.TempDir()
	r := NewReceiver(dir, nil)
	b, _ := json.Marshal(Meta{Name: "x", Size: 10})
	r.HandleMessage(true, b)
	r.HandleMessage(false, []byte("12345"))
	r.Abort("CONNECTION_LOST")
	if res := result(t, r); !IsCode(res.Err, "CONNECTION_LOST") {
		t.Fatalf("err = %v", res.Err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files left behind: %v", entries)
	}
}
