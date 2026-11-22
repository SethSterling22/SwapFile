package clip

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) (*Store, *time.Time) {
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	s := New(t.TempDir())
	s.Now = func() time.Time { return now }
	return s, &now
}

func TestAddTextFileAndBoth(t *testing.T) {
	s, _ := testStore(t)
	src := filepath.Join(t.TempDir(), "informe.pdf")
	os.WriteFile(src, []byte("%PDF"), 0o644)

	if _, err := s.Add("", ""); !errors.Is(err, ErrEmpty) {
		t.Fatalf("empty item: %v", err)
	}
	a, err := s.Add("ssh seth@aery\n", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Add("", src)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Add("el informe de hoy", src) // same file again: its own copy
	if err != nil {
		t.Fatal(err)
	}
	entries, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("%d entries, want 3 (items never replace each other)", len(entries))
	}
	if a.Text != "ssh seth@aery" || a.File != "" {
		t.Fatalf("text item = %+v", a)
	}
	if b.File != "informe.pdf" || c.File != "informe (1).pdf" || c.Text == "" || b.Size != 4 {
		t.Fatalf("file items = %+v / %+v", b, c)
	}
	if b.Type != "application/pdf" {
		t.Fatalf("type %q", b.Type)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("the original file must stay where it was")
	}
}

func TestExpiry(t *testing.T) {
	s, now := testStore(t)
	src := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(src, []byte("x"), 0o644)
	old, _ := s.Add("vieja", src)
	*now = now.Add(23 * time.Hour)
	s.Add("nueva", "")
	if e, _ := s.List(); len(e) != 2 {
		t.Fatalf("before 24 h: %d entries", len(e))
	}
	*now = now.Add(2 * time.Hour) // the first item is now 25 h old
	entries, _ := s.List()
	if len(entries) != 1 || entries[0].Text != "nueva" {
		t.Fatalf("after 24 h: %+v", entries)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, old.File)); !os.IsNotExist(err) {
		t.Fatal("expired attachment was not deleted")
	}
}

func TestLimit(t *testing.T) {
	s, _ := testStore(t)
	s.Limit = 10
	if _, err := s.Add(strings.Repeat("a", 8), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("abc", ""); !errors.Is(err, ErrFull) {
		t.Fatalf("over the limit: %v", err)
	}
	if _, err := s.Add(strings.Repeat("x", MaxText+1), ""); !errors.Is(err, ErrTooLong) {
		t.Fatalf("long text: %v", err)
	}
}

func TestHandCopiedFilesAndRemove(t *testing.T) {
	s, now := testStore(t)
	os.MkdirAll(s.Dir, 0o700)
	os.WriteFile(filepath.Join(s.Dir, "copiado.zip"), []byte("PK"), 0o644)
	os.WriteFile(filepath.Join(s.Dir, ".oculto"), []byte("no"), 0o644)
	entries, _ := s.List()
	if len(entries) != 1 || entries[0].File != "copiado.zip" || !entries[0].Created.Equal(*now) {
		t.Fatalf("hand-copied file: %+v", entries)
	}
	id := entries[0].ID
	if again, _ := s.List(); len(again) != 1 || again[0].ID != id {
		t.Fatal("hand-copied file got a new id on the second listing")
	}

	s.Add("nota", "")
	if _, err := s.Remove("zzzz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove unknown: %v", err)
	}
	if n, err := s.Remove(id[:4]); err != nil || n != 1 {
		t.Fatalf("remove by prefix: %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "copiado.zip")); !os.IsNotExist(err) {
		t.Fatal("removed attachment still there")
	}
	if n, _ := s.Remove("all"); n != 1 {
		t.Fatalf("remove all: %d", n)
	}
	if e, _ := s.List(); len(e) != 0 {
		t.Fatalf("not empty: %+v", e)
	}
}
