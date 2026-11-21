package node

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	res, err := send.Send(ctx, "a", []string{path}, func(done, total int64) { lastDone = done })
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
	if _, err := send.Send(ctx, "A", []string{path}, nil); err != nil {
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
	_, err := send.Send(ctx, "A", []string{path}, nil)
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
	_, err := send.Send(ctx, "A", []string{path}, func(done, total int64) {
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

	_, err := send.Send(ctx, "nobody", []string{path}, nil)
	wantCode(t, err, CodeNotFound)
	_, err = send.Send(ctx, "A", []string{t.TempDir()}, nil)
	wantCode(t, err, CodeNothingToSend)

	recv.stop()
	waitFor(t, func() bool { st := send.Status(); return len(st.Peers) == 1 && !st.Peers[0].Online })
	_, err = send.Send(ctx, "A", []string{path}, nil)
	wantCode(t, err, "DEVICE_OFFLINE")
}

func TestSendFolderAsZip(t *testing.T) {
	_, send, inbox := pairedNodes(t, true)
	root := t.TempDir()
	folder := filepath.Join(root, "proyecto")
	os.MkdirAll(filepath.Join(folder, "src"), 0o755)
	files := map[string][]byte{
		"proyecto/notes.txt":   bytes.Repeat([]byte("hola mundo "), 50000), // compressible
		"proyecto/src/main.go": []byte("package main\n"),
		"proyecto/photo.jpg":   func() []byte { b, _ := os.ReadFile(writeFileOnly(t, 300000)); return b }(),
	}
	for name, data := range files {
		os.WriteFile(filepath.Join(root, name), data, 0o644)
	}
	extra := filepath.Join(root, "extra.pdf")
	os.WriteFile(extra, []byte("%PDF-1.4"), 0o644)
	files["extra.pdf"] = []byte("%PDF-1.4")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var last int64
	res, err := send.Send(ctx, "A", []string{folder, extra}, func(done, total int64) { last = done })
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Items != 4 || !strings.HasPrefix(res.Name, "fileswap-") || !strings.HasSuffix(res.Name, ".zip") {
		t.Fatalf("result = %+v", res)
	}
	var want int64
	for _, d := range files {
		want += int64(len(d))
	}
	if last != want {
		t.Fatalf("progress ended at %d, want %d (bytes of the files)", last, want)
	}
	zr, err := zip.OpenReader(filepath.Join(inbox, res.Name))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != len(files) {
		t.Fatalf("zip has %d entries, want %d", len(zr.File), len(files))
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(rc)
		rc.Close()
		if !bytes.Equal(got, files[f.Name]) {
			t.Errorf("entry %s differs", f.Name)
		}
		wantMethod := zip.Deflate
		if strings.HasSuffix(f.Name, ".jpg") || strings.HasSuffix(f.Name, ".pdf") {
			wantMethod = zip.Store
		}
		if f.Method != wantMethod {
			t.Errorf("entry %s method %d, want %d", f.Name, f.Method, wantMethod)
		}
	}
	if st, _ := os.Stat(filepath.Join(inbox, res.Name)); st.Size() != res.Size {
		t.Fatalf("saved %d bytes, result says %d", st.Size(), res.Size)
	}
}

func writeFileOnly(t *testing.T, size int) string {
	p, _ := writeFile(t, size)
	return p
}

func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}
