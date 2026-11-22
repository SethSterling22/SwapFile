package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/clip"
	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/identity"
)

// clipNodes starts an owner with a clipboard and a viewer with a swap
// folder, already paired.
func clipNodes(t *testing.T, ownerClip bool) (owner, viewer *testNode, store *clip.Store, inbox string) {
	t.Helper()
	srv := testServer(t)
	a, _ := identity.New("dueño", srv)
	b, _ := identity.New("visor", srv)
	a.Pairs = []identity.Pair{{ID: b.ID(), Name: "visor"}}
	b.Pairs = []identity.Pair{{ID: a.ID(), Name: "dueño"}}
	aDir, bDir := t.TempDir(), t.TempDir()
	a.Save(aDir)
	b.Save(bDir)
	opts := Options{}
	if ownerClip {
		store = clip.New(t.TempDir())
		opts.Clip = store
	}
	inbox = t.TempDir()
	owner = runNode(t, aDir, opts)
	viewer = runNode(t, bDir, Options{InboxDir: inbox})
	waitFor(t, func() bool { st := viewer.Status(); return len(st.Peers) == 1 && st.Peers[0].Online })
	return
}

func TestClipboardListAndGet(t *testing.T) {
	_, viewer, store, inbox := clipNodes(t, true)
	src := filepath.Join(t.TempDir(), "plano.pdf")
	os.WriteFile(src, []byte("%PDF-1.7 contenido"), 0o644)
	note, _ := store.Add("ssh seth@aery -p 22", "")
	withFile, _ := store.Add("el plano de la oficina", src)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	list, _, err := viewer.ClipboardOf(ctx, "dueño")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 || list.Limit != clip.DefaultLimit || list.Used != int64(len("ssh seth@aery -p 22")+len("el plano de la oficina")+18) {
		t.Fatalf("list = %+v", list)
	}
	byID := map[string]RemoteItem{}
	for _, it := range list.Items {
		byID[it.ID] = it
	}
	if byID[note.ID].Text != "ssh seth@aery -p 22" || byID[note.ID].File != nil {
		t.Fatalf("note = %+v", byID[note.ID])
	}
	f := byID[withFile.ID].File
	if f == nil || f.Name != "plano.pdf" || f.Size != 18 || f.Type != "application/pdf" {
		t.Fatalf("attachment = %+v", f)
	}
	if exp := time.UnixMilli(byID[note.ID].Expires); time.Until(exp) < 23*time.Hour {
		t.Fatalf("expires %v", exp)
	}

	res, err := viewer.ClipGet(ctx, "dueño", withFile.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(res.Path)
	if string(got) != "%PDF-1.7 contenido" || filepath.Dir(res.Path) != inbox || res.Meta.Clip != withFile.ID {
		t.Fatalf("received %q at %s (%+v)", got, res.Path, res.Meta)
	}

	_, err = viewer.ClipGet(ctx, "dueño", note.ID) // a note has no attachment
	wantCode(t, err, CodeNoItem)
}

func TestClipboardNotShared(t *testing.T) {
	_, viewer, _, _ := clipNodes(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _, err := viewer.ClipboardOf(ctx, "dueño")
	wantCode(t, err, CodeNoClipboard)
}
