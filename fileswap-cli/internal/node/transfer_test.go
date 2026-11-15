package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/identity"
)

// pairedNodes starts two nodes that already list each other as paired.
// The first receives into its own temp inbox; the second only sends.
func pairedNodes(t *testing.T, receiverInbox bool) (recv, send *testNode, inbox string) {
	t.Helper()
	srv := testServer(t)
	a, _ := identity.New("A", srv)
	b, _ := identity.New("B", srv)
	a.Pairs = []identity.Pair{{ID: b.ID(), Name: "B"}}
	b.Pairs = []identity.Pair{{ID: a.ID(), Name: "A"}}
	aDir, bDir := t.TempDir(), t.TempDir()
	if err := a.Save(aDir); err != nil {
		t.Fatal(err)
	}
	if err := b.Save(bDir); err != nil {
		t.Fatal(err)
	}
	opts := Options{}
	if receiverInbox {
		inbox = t.TempDir()
		opts.InboxDir = inbox
	}
	recv = runNode(t, aDir, opts)
	send = runNode(t, bDir, Options{})
	waitFor(t, func() bool { st := send.Status(); return len(st.Peers) == 1 && st.Peers[0].Online })
	return recv, send, inbox
}

func writeFile(t *testing.T, size int) (string, []byte) {
	t.Helper()
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*131 + 17)
	}
	path := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func TestSendFileOverWebRTC(t *testing.T) {
	_, send, inbox := pairedNodes(t, true)
	path, data := writeFile(t, 3<<20+123) // 3 MB, not a multiple of the chunk size

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var lastDone int64
	res, err := send.Send(ctx, "a", path, func(done, total int64) { lastDone = done })
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	sum := sha256.Sum256(data)
	if res.SHA256 != hex.EncodeToString(sum[:]) || res.Size != int64(len(data)) || res.Peer.Name != "A" {
		t.Fatalf("result = %+v", res)
	}
	if lastDone != int64(len(data)) {
		t.Fatalf("progress ended at %d", lastDone)
	}
	got, err := os.ReadFile(filepath.Join(inbox, "backup.tar.gz"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("received file differs (err %v)", err)
	}

	// A second transfer to the same peer gets a new connection and a new name
	if _, err := send.Send(ctx, "A", path, nil); err != nil {
		t.Fatalf("second Send: %v", err)
	}
	if _, err := os.Stat(filepath.Join(inbox, "backup (1).tar.gz")); err != nil {
		entries, _ := os.ReadDir(inbox)
		t.Fatalf("second file: %v (inbox: %v)", err, entries)
	}
}

func TestSendToNodeNotReceiving(t *testing.T) {
	_, send, _ := pairedNodes(t, false)
	path, _ := writeFile(t, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := send.Send(ctx, "A", path, nil)
	var e *Error
	if !asError(err, &e) || e.Code != CodePeerAborted || e.Detail != abortNotReceiving {
		t.Fatalf("err = %v, want PEER_ABORTED NOT_RECEIVING", err)
	}
}

func TestSendCancelLeavesNothing(t *testing.T) {
	_, send, inbox := pairedNodes(t, true)
	path, _ := writeFile(t, 64<<20) // big enough to still be running when cancelled
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	var once bool
	go func() {
		<-started
		cancel()
	}()
	_, err := send.Send(ctx, "A", path, func(done, total int64) {
		if !once && done > 1<<20 {
			once = true
			close(started)
		}
	})
	if err == nil {
		t.Fatal("cancelled Send succeeded")
	}
	waitFor(t, func() bool {
		entries, _ := os.ReadDir(inbox)
		return len(entries) == 0
	})
}

func TestSendErrors(t *testing.T) {
	recv, send, _ := pairedNodes(t, true)
	path, _ := writeFile(t, 10)
	ctx := context.Background()

	_, err := send.Send(ctx, "nobody", path, nil)
	wantCode(t, err, CodeNotFound)
	_, err = send.Send(ctx, "A", t.TempDir(), nil)
	wantCode(t, err, CodeNotFile)

	recv.stop()
	waitFor(t, func() bool { st := send.Status(); return len(st.Peers) == 1 && !st.Peers[0].Online })
	_, err = send.Send(ctx, "A", path, nil)
	wantCode(t, err, "DEVICE_OFFLINE")
}

func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}
