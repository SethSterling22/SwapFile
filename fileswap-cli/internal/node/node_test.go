package node

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/identity"
)

// These tests run against the real signaling server (fileswap/server). Set
// FILESWAP_TEST_SERVER=ws://host:port/ws to use a running one; otherwise
// it is started with node, and the tests are skipped if node is missing.

func testServer(t *testing.T) string {
	t.Helper()
	if url := os.Getenv("FILESWAP_TEST_SERVER"); url != "" {
		return url
	}
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed and FILESWAP_TEST_SERVER not set")
	}
	script, _ := filepath.Abs("../../../fileswap/server/index.js")
	if _, err := os.Stat(filepath.Join(filepath.Dir(script), "node_modules")); err != nil {
		t.Skip("fileswap/server dependencies not installed (npm ci)")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cmd := exec.Command(nodeBin, script)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", port))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	for i := 0; i < 50; i++ {
		if resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port)); err == nil {
			resp.Body.Close()
			return fmt.Sprintf("ws://127.0.0.1:%d/ws", port)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("signaling server did not start")
	return ""
}

type testNode struct {
	*Node
	dir    string
	cancel context.CancelFunc
	done   chan error
}

// startNode creates an identity in a temp dir and brings a node online.
func startNode(t *testing.T, server, name string, opts Options, setup func(*identity.Identity)) *testNode {
	t.Helper()
	dir := t.TempDir()
	id, err := identity.New(name, server)
	if err != nil {
		t.Fatal(err)
	}
	if setup != nil {
		setup(id)
	}
	if err := id.Save(dir); err != nil {
		t.Fatal(err)
	}
	return runNode(t, dir, opts)
}

func runNode(t *testing.T, dir string, opts Options) *testNode {
	t.Helper()
	opts.Dir = dir
	opts.Log = func(ev string, a ...any) { t.Logf("[%s] %s %v", filepath.Base(dir), ev, a) }
	n, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	tn := &testNode{Node: n, dir: dir, cancel: cancel, done: make(chan error, 1)}
	go func() { tn.done <- n.Run(ctx) }()
	t.Cleanup(tn.stop)
	wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
	defer wcancel()
	if err := n.WaitOnline(wctx); err != nil {
		t.Fatalf("node did not come online: %v", err)
	}
	return tn
}

func (tn *testNode) stop() { tn.cancel() }

func (tn *testNode) pairs(t *testing.T) []identity.Pair {
	t.Helper()
	id, err := identity.Load(tn.dir)
	if err != nil {
		t.Fatal(err)
	}
	return id.Pairs
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("err = %v, want %s", err, code)
	}
}

func syncCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPairWithAllowListAndUnpair(t *testing.T) {
	srv := testServer(t)
	bID, _ := identity.New("B", srv)
	a := startNode(t, srv, "A", Options{HandleRequests: true}, func(id *identity.Identity) { id.Allow = []string{bID.ID()} })
	bDir := t.TempDir()
	if err := bID.Save(bDir); err != nil {
		t.Fatal(err)
	}
	b := runNode(t, bDir, Options{})

	name, err := b.Sync(syncCtx(t), a.ID())
	if err != nil || name != "A" {
		t.Fatalf("Sync = %q, %v", name, err)
	}
	if p := b.pairs(t); len(p) != 1 || p[0].ID != a.ID() || p[0].Name != "A" {
		t.Fatalf("B pairs = %+v", p)
	}
	waitFor(t, func() bool { p := a.pairs(t); return len(p) == 1 && p[0].ID == b.ID() })
	waitFor(t, func() bool { st := b.Status(); return len(st.Peers) == 1 && st.Peers[0].Online })

	// Pairing again is refused locally
	_, err = b.Sync(syncCtx(t), a.ID())
	wantCode(t, err, CodeAlreadyPaired)

	// Unpair by name removes it on both sides
	if _, err := b.Unpair(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if p := b.pairs(t); len(p) != 0 {
		t.Fatalf("B pairs after unpair = %+v", p)
	}
	waitFor(t, func() bool { return len(a.pairs(t)) == 0 })
}

func TestPairAskAndReject(t *testing.T) {
	srv := testServer(t)
	asked := make(chan string, 1)
	a := startNode(t, srv, "A", Options{HandleRequests: true, Decide: func(_ context.Context, code, name string) bool {
		asked <- name
		return false
	}}, nil)
	b := startNode(t, srv, "B", Options{}, nil)

	_, err := b.Sync(syncCtx(t), a.ID())
	wantCode(t, err, CodeRejected)
	if got := <-asked; got != "B" {
		t.Fatalf("Decide asked about %q", got)
	}
	if len(a.pairs(t)) != 0 || len(b.pairs(t)) != 0 {
		t.Fatal("rejected pairing was stored")
	}
}

func TestPairNotAllowedWithoutDecide(t *testing.T) {
	srv := testServer(t)
	a := startNode(t, srv, "A", Options{HandleRequests: true}, nil)
	b := startNode(t, srv, "B", Options{}, nil)
	_, err := b.Sync(syncCtx(t), a.ID())
	wantCode(t, err, CodeRejected)
}

func TestSyncErrors(t *testing.T) {
	srv := testServer(t)
	b := startNode(t, srv, "B", Options{}, nil)

	_, err := b.Sync(syncCtx(t), "00000000-0000-4000-8000-000000000000")
	wantCode(t, err, "DEVICE_OFFLINE")
	_, err = b.Sync(syncCtx(t), b.ID())
	wantCode(t, err, CodeSelf)
	_, err = b.Unpair(context.Background(), "nobody")
	wantCode(t, err, CodeNotFound)
}

func TestControlSocket(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	srv := testServer(t)
	bID, _ := identity.New("B", srv)
	a := startNode(t, srv, "A", Options{HandleRequests: true}, func(id *identity.Identity) { id.Allow = []string{bID.ID()} })
	bDir := t.TempDir()
	if err := bID.Save(bDir); err != nil {
		t.Fatal(err)
	}
	b := runNode(t, bDir, Options{HandleRequests: true})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := b.ListenControl(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go b.ServeControl(ctx, ln, func() { close(stopped) })
	path := SocketPath(b.ID())

	// A second listen for the same identity is refused
	if _, err := b.ListenControl(ctx); err == nil {
		t.Fatal("second ListenControl succeeded")
	} else {
		wantCode(t, err, CodeAlreadyRunning)
	}

	resp, err := Call(ctx, path, Request{Cmd: "status"})
	if err != nil || resp.Status == nil || !resp.Status.Online || resp.Status.ID != b.ID() {
		t.Fatalf("status = %+v, %v", resp, err)
	}
	resp, err = Call(ctx, path, Request{Cmd: "sync", Arg: a.ID()})
	if err != nil || !resp.OK || resp.Name != "A" {
		t.Fatalf("sync = %+v, %v", resp, err)
	}
	resp, err = Call(ctx, path, Request{Cmd: "unpair", Arg: "nobody"})
	if err != nil || resp.OK || resp.Error != CodeNotFound {
		t.Fatalf("unpair nobody = %+v, %v", resp, err)
	}
	if _, err := Call(ctx, path, Request{Cmd: "stop"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not call stop()")
	}
	cancel()
	waitFor(t, func() bool { _, err := os.Stat(path); return errors.Is(err, os.ErrNotExist) })
	if _, err := Call(context.Background(), path, Request{Cmd: "status"}); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Call after stop: %v", err)
	}
}

func TestReplacedByAnotherConnection(t *testing.T) {
	srv := testServer(t)
	first := startNode(t, srv, "A", Options{}, nil)
	runNode(t, first.dir, Options{}) // same identity connects again
	select {
	case err := <-first.done:
		wantCode(t, err, CodeReplaced)
	case <-time.After(5 * time.Second):
		t.Fatal("first connection was not replaced")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("condition not met in 5s")
}
