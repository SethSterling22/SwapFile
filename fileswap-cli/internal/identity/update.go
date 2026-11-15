package identity

import (
	"os"
	"path/filepath"
	"syscall"
)

// Update loads the identity, applies fn and saves it, holding an exclusive
// lock on identity.lock so a running `listen` and a command like `allow`
// never overwrite each other's changes. fn must not call Update.
func Update(dir string, fn func(*Identity) error) (*Identity, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "identity.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	id, err := Load(dir)
	if err != nil {
		return nil, err
	}
	if err := fn(id); err != nil {
		return nil, err
	}
	if err := id.Save(dir); err != nil {
		return nil, err
	}
	return id, nil
}

// PairIDs returns the codes of the paired devices.
func (id *Identity) PairIDs() []string {
	ids := make([]string, 0, len(id.Pairs))
	for _, p := range id.Pairs {
		ids = append(ids, p.ID)
	}
	return ids
}

// SetPair adds a pair or updates its name. It reports whether anything changed.
func (id *Identity) SetPair(code, name string) bool {
	for i, p := range id.Pairs {
		if p.ID == code {
			if name == "" || p.Name == name {
				return false
			}
			id.Pairs[i].Name = name
			return true
		}
	}
	id.Pairs = append(id.Pairs, Pair{ID: code, Name: name})
	return true
}

// RemovePair removes a pair. It reports whether it was there.
func (id *Identity) RemovePair(code string) bool {
	for i, p := range id.Pairs {
		if p.ID == code {
			id.Pairs = append(id.Pairs[:i], id.Pairs[i+1:]...)
			return true
		}
	}
	return false
}

// HasPair reports whether code is a paired device.
func (id *Identity) HasPair(code string) bool {
	for _, p := range id.Pairs {
		if p.ID == code {
			return true
		}
	}
	return false
}

// Allowed reports whether code is in the allow list.
func (id *Identity) Allowed(code string) bool {
	for _, a := range id.Allow {
		if a == code {
			return true
		}
	}
	return false
}
