package identity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Vector computed with the server's idFromSecret (fileswap/server/index.js).
func TestDeviceIDMatchesServer(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if got, want := DeviceID(secret), "a8ae6e6e-e929-abea-3afc-fc5258c8ccd6"; got != want {
		t.Fatalf("DeviceID = %s, want %s", got, want)
	}
}

func TestNewSecretFormat(t *testing.T) {
	id, err := New("srv", "ws://localhost:7341/ws")
	if err != nil {
		t.Fatal(err)
	}
	if err := id.Validate(); err != nil {
		t.Fatal(err)
	}
	if !IsDeviceID(id.ID()) {
		t.Fatalf("bad device id %q", id.ID())
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fileswap")
	if _, err := Load(dir); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("Load on empty dir: %v, want ErrNotInitialized", err)
	}
	id, _ := New("srv", "ws://localhost:7341/ws")
	id.Pairs = append(id.Pairs, Pair{ID: "a8ae6e6e-e929-abea-3afc-fc5258c8ccd6", Name: "Cell"})
	if err := id.Save(dir); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Fatalf("identity file mode %o, want 600", perm)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != id.ID() || len(got.Pairs) != 1 || got.Pairs[0].Name != "Cell" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestFind(t *testing.T) {
	id := &Identity{Pairs: []Pair{
		{ID: "a8ae6e6e-e929-abea-3afc-fc5258c8ccd6", Name: "Cell"},
		{ID: "a8b00000-0000-4000-8000-000000000000", Name: "Laptop"},
		{ID: "cd84b9ef-1111-2222-3333-444455556666", Name: "laptop"},
	}}
	cases := []struct {
		q, want   string
		ambiguous bool
	}{
		{q: "a8ae6e6e-e929-abea-3afc-fc5258c8ccd6", want: "Cell"},
		{q: "A8AE", want: "Cell"},
		{q: "cell", want: "Cell"},
		{q: "cd84", want: "laptop"},
		{q: "a8", ambiguous: true},
		{q: "Laptop", ambiguous: true},
		{q: "nope"},
	}
	for _, c := range cases {
		p, err := id.Find(c.q)
		var amb *ErrAmbiguous
		switch {
		case c.ambiguous:
			if !errors.As(err, &amb) {
				t.Errorf("Find(%q): err %v, want ambiguous", c.q, err)
			}
		case c.want == "":
			if p != nil || err != nil {
				t.Errorf("Find(%q) = %v, %v; want no match", c.q, p, err)
			}
		default:
			if err != nil || p == nil || p.Name != c.want {
				t.Errorf("Find(%q) = %v, %v; want %s", c.q, p, err, c.want)
			}
		}
	}
}
