package transfer

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCollectEntries(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "docs", "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "docs", "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(root, "docs", "sub", "b.txt"), []byte("bb"), 0o644)
	os.Symlink("/etc/passwd", filepath.Join(root, "docs", "link"))
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("top"), 0o644)
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "a.txt"), []byte("other"), 0o644)

	entries, err := CollectEntries([]string{filepath.Join(root, "docs"), filepath.Join(root, "a.txt"), filepath.Join(other, "a.txt")})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	want := []string{"a (1).txt", "a.txt", "docs/a.txt", "docs/sub/b.txt"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("names = %v, want %v (symlink skipped, duplicates renamed)", names, want)
	}
	if TotalSize(entries) != 1+2+3+5 {
		t.Fatalf("TotalSize = %d", TotalSize(entries))
	}
	if _, err := CollectEntries([]string{"/dev/null"}); err == nil {
		t.Fatal("device accepted")
	}
}

func TestArchiveName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Mis Fotos")
	os.Mkdir(dir, 0o755)
	if got := ArchiveName([]string{dir}, time.Now()); got != "Mis Fotos.zip" {
		t.Fatalf("folder: %q", got)
	}
	now := time.Date(2026, 11, 21, 3, 52, 7, 0, time.UTC)
	if got := ArchiveName([]string{dir, dir}, now); got != "fileswap-2026-11-21-035207.zip" {
		t.Fatalf("several: %q", got)
	}
}

func TestWriteZipRoundTrip(t *testing.T) {
	root := t.TempDir()
	text := bytes.Repeat([]byte("compressible text "), 10000)
	os.WriteFile(filepath.Join(root, "t.txt"), text, 0o644)
	os.WriteFile(filepath.Join(root, "p.JPG"), []byte{1, 2, 3}, 0o644)
	entries, _ := CollectEntries([]string{filepath.Join(root, "t.txt"), filepath.Join(root, "p.JPG")})
	var buf bytes.Buffer
	var read atomic.Int64
	if err := WriteZip(context.Background(), &buf, entries, &read); err != nil {
		t.Fatal(err)
	}
	if read.Load() != int64(len(text)+3) {
		t.Fatalf("read %d", read.Load())
	}
	if buf.Len() >= len(text) {
		t.Fatalf("text was not compressed: %d bytes", buf.Len())
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		rc, _ := f.Open()
		got, _ := io.ReadAll(rc)
		rc.Close()
		switch f.Name {
		case "t.txt":
			if !bytes.Equal(got, text) || f.Method != zip.Deflate {
				t.Errorf("t.txt: method %d, equal %v", f.Method, bytes.Equal(got, text))
			}
		case "p.JPG":
			if !bytes.Equal(got, []byte{1, 2, 3}) || f.Method != zip.Store {
				t.Errorf("p.JPG: method %d", f.Method)
			}
		default:
			t.Errorf("unexpected entry %s", f.Name)
		}
	}
}

func TestStreamUnknownSize(t *testing.T) {
	dir := t.TempDir()
	p := newPipe(dir)
	data := payload(3*ChunkSize + 7)
	sum, err := p.send.Send(context.Background(), bytes.NewReader(data), Meta{Name: "x.zip", Size: UnknownSize, Approx: int64(len(data))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := result(t, p.recv)
	if res.Err != nil || res.SHA256 != sum || res.Meta.Size != int64(len(data)) {
		t.Fatalf("res = %+v", res)
	}
	got, _ := os.ReadFile(res.Path)
	if !bytes.Equal(got, data) {
		t.Fatal("content differs")
	}

	// A truncated stream (end.size larger than received) is rejected
	p2 := newPipe(t.TempDir())
	p2.tamper = func(isString bool, b []byte) []byte {
		if !isString && len(b) == ChunkSize {
			return nil // drop full chunks
		}
		return b
	}
	_, err = p2.send.Send(context.Background(), bytes.NewReader(data), Meta{Name: "x.zip", Size: UnknownSize, Approx: int64(len(data))}, nil)
	if !IsCode(err, ErrSizeMismatch) {
		t.Fatalf("truncated stream: %v", err)
	}
}
