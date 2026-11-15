package transfer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxNameBytes = 255

// SafeName turns the file name announced by a remote peer into a plain file
// name that cannot escape the receive directory: directory components are
// dropped, control characters removed, and leading dots stripped so a peer
// cannot plant hidden files such as ".bashrc" or ".ssh".
func SafeName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	name = strings.TrimSpace(name)
	if name == "" {
		name = "file"
	}
	return truncate(name, maxNameBytes)
}

// splitExt returns the extension to keep after " (n)", including compound
// ones like ".tar.gz" so "backup.tar.gz" becomes "backup (1).tar.gz".
func splitExt(name string) string {
	ext := filepath.Ext(name)
	if inner := filepath.Ext(strings.TrimSuffix(name, ext)); strings.EqualFold(inner, ".tar") && len(ext) <= 5 {
		return inner + ext
	}
	return ext
}

// truncate shortens name to at most n bytes on a rune boundary, keeping a
// short extension when there is one.
func truncate(name string, n int) string {
	if len(name) <= n {
		return name
	}
	ext := filepath.Ext(name)
	if len(ext) > 16 {
		ext = ""
	}
	base := name[:len(name)-len(ext)]
	for len(base)+len(ext) > n {
		_, size := utf8.DecodeLastRuneInString(base)
		base = base[:len(base)-size]
	}
	return base + ext
}

// CreateUnique creates a new file for name in dir without overwriting
// anything, adding " (1)", " (2)"… before the extension when needed.
// name must already be a SafeName.
func CreateUnique(dir, name string) (*os.File, error) {
	ext := splitExt(name)
	base := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		candidate := name
		if i > 0 {
			candidate = truncate(fmt.Sprintf("%s (%d)%s", base, i, ext), maxNameBytes)
		}
		f, err := os.OpenFile(filepath.Join(dir, candidate), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return f, err
	}
	return nil, fmt.Errorf("too many files named %q in %s", name, dir)
}
