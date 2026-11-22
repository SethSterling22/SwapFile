// Package clip is this node's clipboard: items that paired devices can see
// and take while the node is online (docs/protocol.md §5).
//
// An item has a text (a link, a command, a note), an attached file, or
// both. Items are independent: adding one never replaces another. The
// clipboard lives in <swap folder>/Clipboard: attachments are plain files
// there, and .clipboard.json keeps the texts and when each item was added.
// A file copied into the folder by hand becomes an item too. Items expire
// after TTL (they are deleted, attachment included) and the clipboard holds
// at most Limit bytes.
package clip

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/transfer"
)

const (
	// DefaultLimit is the total size of a clipboard (texts + attachments).
	DefaultLimit = 500 << 20
	// DefaultTTL is how long an item stays.
	DefaultTTL = 24 * time.Hour
	// MaxText is the longest text of one item.
	MaxText = 64 << 10
	// MaxItems is the most items a clipboard holds.
	MaxItems = 100

	// DirName is the clipboard folder inside the swap folder.
	DirName   = "Clipboard"
	indexName = ".clipboard.json"
)

var (
	ErrEmpty    = errors.New("an item needs a text, a file or both")
	ErrTooLong  = errors.New("text too long")
	ErrFull     = errors.New("clipboard full")
	ErrNotFound = errors.New("no such item")
)

// Item is one clipboard entry.
type Item struct {
	ID      string    `json:"id"`
	Text    string    `json:"text,omitempty"`
	File    string    `json:"file,omitempty"` // attachment, a file name inside the clipboard folder
	Created time.Time `json:"created"`
}

// Store is a clipboard folder.
type Store struct {
	Dir   string
	Limit int64
	TTL   time.Duration
	Now   func() time.Time // for tests
}

// New returns the clipboard inside a swap folder, with the default limits.
func New(inbox string) *Store {
	return &Store{Dir: filepath.Join(inbox, DirName), Limit: DefaultLimit, TTL: DefaultTTL}
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Entry is an item with its attachment's details, as listed.
type Entry struct {
	Item
	Size    int64     `json:"size,omitempty"`
	Type    string    `json:"type,omitempty"`
	Expires time.Time `json:"expires"`
	Path    string    `json:"-"`
}

// Bytes is what the item counts against the limit.
func (e Entry) Bytes() int64 { return e.Size + int64(len(e.Text)) }

// List returns the current items, newest first. It also removes expired
// items, adopts files copied into the folder by hand, and forgets items
// whose attachment was deleted.
func (s *Store) List() ([]Entry, error) {
	var out []Entry
	err := s.update(func(items *[]Item) error {
		out = s.sync(items)
		return nil
	})
	return out, err
}

// Used is the size of the listed entries.
func Used(entries []Entry) int64 {
	var n int64
	for _, e := range entries {
		n += e.Bytes()
	}
	return n
}

// Add creates an item with a text, a copy of the file at src, or both.
func (s *Store) Add(text, src string) (Entry, error) {
	text = strings.TrimRight(text, "\n")
	if text == "" && src == "" {
		return Entry{}, ErrEmpty
	}
	if len(text) > MaxText || !utf8.ValidString(text) {
		return Entry{}, ErrTooLong
	}
	var size int64
	if src != "" {
		st, err := os.Stat(src)
		if err != nil {
			return Entry{}, err
		}
		if !st.Mode().IsRegular() {
			return Entry{}, fmt.Errorf("%s: %w", src, transfer.ErrNotRegular)
		}
		size = st.Size()
	}

	var added Entry
	err := s.update(func(items *[]Item) error {
		entries := s.sync(items)
		if len(entries) >= MaxItems || Used(entries)+size+int64(len(text)) > s.Limit {
			return ErrFull
		}
		it := Item{ID: newID(), Text: text, Created: s.now()}
		if src != "" {
			f, err := copyInto(s.Dir, src)
			if err != nil {
				return err
			}
			it.File = f
		}
		*items = append(*items, it)
		added = s.entry(it)
		return nil
	})
	return added, err
}

// Remove deletes an item (and its attachment) by id or unique id prefix.
// "all" empties the clipboard.
func (s *Store) Remove(ref string) (int, error) {
	removed := 0
	err := s.update(func(items *[]Item) error {
		s.sync(items)
		keep := (*items)[:0]
		matches := 0
		for _, it := range *items {
			if ref == "all" || strings.HasPrefix(it.ID, strings.ToLower(ref)) {
				matches++
			}
		}
		if ref != "all" && matches != 1 {
			return ErrNotFound
		}
		for _, it := range *items {
			if ref == "all" || strings.HasPrefix(it.ID, strings.ToLower(ref)) {
				if it.File != "" {
					os.Remove(filepath.Join(s.Dir, it.File))
				}
				removed++
				continue
			}
			keep = append(keep, it)
		}
		*items = keep
		return nil
	})
	return removed, err
}

// Get returns an item by exact id.
func (s *Store) Get(id string) (Entry, error) {
	entries, err := s.List()
	if err != nil {
		return Entry{}, err
	}
	for _, e := range entries {
		if e.ID == id {
			return e, nil
		}
	}
	return Entry{}, ErrNotFound
}

// sync brings the index in line with the folder and returns the entries
// that are shared, newest first, within the limit.
func (s *Store) sync(items *[]Item) []Entry {
	now := s.now()
	files := map[string]bool{}
	if dirents, err := os.ReadDir(s.Dir); err == nil {
		for _, d := range dirents {
			if d.Type().IsRegular() && !strings.HasPrefix(d.Name(), ".") {
				files[d.Name()] = true
			}
		}
	}
	known := map[string]bool{}
	keep := (*items)[:0]
	for _, it := range *items {
		switch {
		case now.Sub(it.Created) >= s.TTL:
			if it.File != "" {
				os.Remove(filepath.Join(s.Dir, it.File))
				delete(files, it.File) // gone, not a hand-copied file
			}
		case it.File != "" && !files[it.File]:
			if it.Text != "" { // attachment deleted by hand: keep the text
				it.File = ""
				keep = append(keep, it)
			}
		default:
			if it.File != "" {
				known[it.File] = true
			}
			keep = append(keep, it)
		}
	}
	// Files copied into the folder by hand: their 24 h start now
	for name := range files {
		if !known[name] {
			keep = append(keep, Item{ID: newID(), File: name, Created: now})
		}
	}
	*items = keep

	entries := make([]Entry, 0, len(keep))
	for _, it := range keep {
		entries = append(entries, s.entry(it))
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Created.After(entries[j].Created) })
	// Newest first, up to the limit; older ones beyond it are not shared
	var used int64
	shared := entries[:0]
	for _, e := range entries {
		if len(shared) >= MaxItems || used+e.Bytes() > s.Limit {
			continue
		}
		used += e.Bytes()
		shared = append(shared, e)
	}
	return shared
}

func (s *Store) entry(it Item) Entry {
	e := Entry{Item: it, Expires: it.Created.Add(s.TTL)}
	if it.File != "" {
		e.Path = filepath.Join(s.Dir, it.File)
		if st, err := os.Stat(e.Path); err == nil {
			e.Size = st.Size()
		}
		e.Type = mime.TypeByExtension(filepath.Ext(it.File))
		if e.Type == "" {
			e.Type = "application/octet-stream"
		}
	}
	return e
}

// update loads the index, applies fn and saves it under an exclusive lock.
func (s *Store) update(fn func(*[]Item) error) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(s.Dir, ".clipboard.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	var items []Item
	if b, err := os.ReadFile(filepath.Join(s.Dir, indexName)); err == nil {
		_ = json.Unmarshal(b, &items)
	}
	if err := fn(&items); err != nil {
		return err
	}
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.Dir, indexName+".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Dir, indexName))
}

// copyInto copies src into dir under a free name and returns that name.
func copyInto(dir, src string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := transfer.CreateUnique(dir, transfer.SafeName(filepath.Base(src)))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(out.Name())
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(out.Name())
		return "", err
	}
	return filepath.Base(out.Name()), nil
}

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
