package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":                 "report.pdf",
		"../../.ssh/authorized_keys": "authorized_keys",
		`..\..\Windows\evil.exe`:     "evil.exe",
		"/etc/passwd":                "passwd",
		".bashrc":                    "bashrc",
		"..":                         "file",
		"":                           "file",
		"  spaced name.txt  ":        "spaced name.txt",
		"new\nline\x00.txt":          "newline.txt",
		"foto año 2026.jpg":          "foto año 2026.jpg",
		"dir/":                       "file",
	}
	for in, want := range cases {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeNameLength(t *testing.T) {
	long := strings.Repeat("ñ", 300) + ".tar.gz"
	got := SafeName(long)
	if len(got) > maxNameBytes || !strings.HasSuffix(got, ".gz") || !strings.HasPrefix(got, "ñ") {
		t.Fatalf("SafeName(long) = %d bytes %q", len(got), got)
	}
}

func TestCreateUnique(t *testing.T) {
	dir := t.TempDir()
	var names []string
	for i := 0; i < 3; i++ {
		f, err := CreateUnique(dir, "a.txt")
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, filepath.Base(f.Name()))
		f.Close()
	}
	want := []string{"a.txt", "a (1).txt", "a (2).txt"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "a (2).txt")); err != nil {
		t.Fatal(err)
	}
}

// The browser client parses these frames; keep the JSON shapes stable.
func TestFrameJSON(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{Meta{V: 1, Name: "a.txt", Size: 3, Type: "text/plain"}, `{"v":1,"name":"a.txt","size":3,"type":"text/plain"}`},
		{End{End: true}, `{"__end":true}`},
		{End{End: true, SHA256: "ab"}, `{"__end":true,"sha256":"ab"}`},
		{Ack{Ack: true, OK: true, Received: 3}, `{"__ack":true,"ok":true,"received":3}`},
		{Ack{Ack: true, Received: 2, Error: ErrSizeMismatch}, `{"__ack":true,"ok":false,"received":2,"error":"SIZE_MISMATCH"}`},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.v)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != c.want {
			t.Errorf("json = %s, want %s", b, c.want)
		}
	}
}
