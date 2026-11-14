// Package identity stores this node's FileSwap identity: the private device
// secret, the public device code derived from it, and the paired devices.
//
// The device code is derived exactly like the signaling server does
// (fileswap/server/index.js, idFromSecret): SHA-256 of the hex secret,
// formatted as a UUID. Only the secret is persisted; the code is recomputed.
package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FileName is the identity file inside the config directory.
const FileName = "identity.json"

var (
	secretRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	idRE     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	// ErrNotInitialized means there is no identity file yet (run `fileswap init`).
	ErrNotInitialized = errors.New("identity not initialized")
)

// Pair is a device this node is paired with.
type Pair struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Identity is the persisted state of this node.
type Identity struct {
	Version int    `json:"version"`
	Secret  string `json:"secret"`
	Name    string `json:"name"`
	Server  string `json:"server"`
	Pairs   []Pair `json:"pairs"`
	// Allow lists device codes whose pairing requests `listen` accepts without asking.
	Allow []string `json:"allow"`
}

// New creates an identity with a fresh random 256-bit secret.
func New(name, server string) (*Identity, error) {
	secret, err := GenSecret()
	if err != nil {
		return nil, err
	}
	return &Identity{Version: 1, Secret: secret, Name: name, Server: server, Pairs: []Pair{}, Allow: []string{}}, nil
}

// GenSecret returns 32 random bytes as lowercase hex, the format the server expects.
func GenSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// DeviceID derives the public device code from a secret.
func DeviceID(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	h := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

// ID returns this node's public device code.
func (id *Identity) ID() string { return DeviceID(id.Secret) }

// IsDeviceID reports whether s is a well-formed device code.
func IsDeviceID(s string) bool { return idRE.MatchString(s) }

// ErrAmbiguous is returned by Find when a prefix or name matches several pairs.
type ErrAmbiguous struct {
	Query   string
	Matches []Pair
}

func (e *ErrAmbiguous) Error() string {
	return fmt.Sprintf("%q matches %d paired devices; use a longer code", e.Query, len(e.Matches))
}

// Find resolves a node reference (full code, code prefix, or exact name) to a pair.
// It returns nil, nil when nothing matches.
func (id *Identity) Find(query string) (*Pair, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}
	var byID, byName []Pair
	for _, p := range id.Pairs {
		if p.ID == q {
			p := p
			return &p, nil
		}
		if strings.HasPrefix(p.ID, q) {
			byID = append(byID, p)
		}
		if strings.EqualFold(p.Name, strings.TrimSpace(query)) {
			byName = append(byName, p)
		}
	}
	for _, m := range [][]Pair{byID, byName} {
		switch len(m) {
		case 0:
			continue
		case 1:
			return &m[0], nil
		default:
			return nil, &ErrAmbiguous{Query: query, Matches: m}
		}
	}
	return nil, nil
}

// Validate checks the fields loaded from disk.
func (id *Identity) Validate() error {
	if !secretRE.MatchString(id.Secret) {
		return errors.New("invalid secret in identity file")
	}
	if strings.TrimSpace(id.Name) == "" {
		return errors.New("empty device name in identity file")
	}
	for _, p := range id.Pairs {
		if !IsDeviceID(p.ID) {
			return fmt.Errorf("invalid paired device code %q in identity file", p.ID)
		}
	}
	return nil
}

// Load reads the identity from dir.
func Load(dir string) (*Identity, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotInitialized
	}
	if err != nil {
		return nil, err
	}
	var id Identity
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, fmt.Errorf("read %s: %w", FileName, err)
	}
	if id.Pairs == nil {
		id.Pairs = []Pair{}
	}
	if id.Allow == nil {
		id.Allow = []string{}
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}
	return &id, nil
}

// Save writes the identity to dir atomically. The file holds the device
// secret, so the directory is 0700 and the file 0600.
func (id *Identity) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, FileName+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, FileName))
}

// DefaultDir returns $FILESWAP_CONFIG, else $XDG_CONFIG_HOME/fileswap, else ~/.config/fileswap.
func DefaultDir() (string, error) {
	if d := os.Getenv("FILESWAP_CONFIG"); d != "" {
		return d, nil
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "fileswap"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "fileswap"), nil
}
