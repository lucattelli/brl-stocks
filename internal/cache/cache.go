// Package cache holds the two shared files and the lock (D54, D56).
package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/lucattelli/brl-stocks/internal/market"
)

// Files is the folder the tool keeps its files in (spec 7.7).
type Files struct{ Dir string }

const (
	quotesName = "cache.v1.json"
	sourceName = "source.v1.json"
	lockName   = "lock"
	schema     = 1
)

type quotesFile struct {
	Schema  int             `json:"schema"`
	Entries market.Snapshot `json:"entries"`
}

type sourceFile struct {
	Schema  int                      `json:"schema"`
	Sources map[string]market.Health `json:"sources"`
}

// Load reads the quote cache without locking. A missing, empty, corrupt or
// other-schema file is an empty cache, silently (D48).
func (f Files) Load() market.Snapshot {
	var q quotesFile
	data, err := os.ReadFile(filepath.Join(f.Dir, quotesName))
	if err != nil || json.Unmarshal(data, &q) != nil || q.Schema != schema || q.Entries == nil {
		return market.Snapshot{}
	}
	return q.Entries
}

// LoadSource reads the block record of one source without locking.
func (f Files) LoadSource(name string) market.Health {
	var s sourceFile
	data, err := os.ReadFile(filepath.Join(f.Dir, sourceName))
	if err != nil || json.Unmarshal(data, &s) != nil || s.Schema != schema {
		return market.Health{}
	}
	return s.Sources[name]
}

// ErrBusy means the lock was not obtained within its short wait.
var ErrBusy = errors.New("another process holds the lock")

// Lock takes the lock for one read-modify-write. It never waits long: it is
// tried without blocking, again every 50 milliseconds, for at most one
// second (protocol rule 1).
func (f Files) Lock(after func(time.Duration) <-chan time.Time) (unlock func(), err error) {
	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(f.Dir, lockName), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	for try := 0; ; try++ {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				_ = file.Close()
			}, nil
		}
		if try >= 20 {
			_ = file.Close()
			return nil, ErrBusy
		}
		<-after(50 * time.Millisecond)
	}
}

// Write replaces the quote cache. The caller holds the lock.
func (f Files) Write(s market.Snapshot) error {
	return f.replace(quotesName, quotesFile{Schema: schema, Entries: s})
}

// WriteSource replaces the block record of one source. The caller holds the lock.
func (f Files) WriteSource(name string, h market.Health) error {
	var s sourceFile
	data, err := os.ReadFile(filepath.Join(f.Dir, sourceName))
	if err != nil || json.Unmarshal(data, &s) != nil || s.Schema != schema || s.Sources == nil {
		s = sourceFile{Schema: schema, Sources: map[string]market.Health{}}
	}
	s.Sources[name] = h
	return f.replace(sourceName, s)
}

// replace writes a temp file in the same folder and renames it over the
// target, so a reader sees the old file or the new one, never half of one.
func (f Files) replace(name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(f.Dir, name+".*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(f.Dir, name)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
