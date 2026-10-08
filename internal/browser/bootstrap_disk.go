package browser

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/moreveal/mimic/internal/engine"
)

const (
	bootstrapDiskSchema = "mimic-bootstrap-v1"
	bootstrapDiskMaxAge = 30 * 24 * time.Hour
	bootstrapDiskKeep   = 8
	bootstrapDiskHeader = 16 + sha256.Size + 8
)

var bootstrapDiskMagic = [16]byte{'M', 'I', 'M', 'I', 'C', 'B', 'O', 'O', 'T', 'S', 'T', 'R', 'A', 'P', 1, 0}

type bootstrapDiskStore struct {
	dir      string
	factory  engine.PersistentBootstrapSnapshotFactory
	identity string
}

func newBootstrapDiskStore(dir string, factory engine.Factory) (*bootstrapDiskStore, error) {
	persistent, ok := factory.(engine.PersistentBootstrapSnapshotFactory)
	if !ok || !persistent.BootstrapSnapshotsEnabled() {
		return nil, nil
	}
	identity, err := persistent.BootstrapSnapshotIdentity()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	store := &bootstrapDiskStore{dir: dir, factory: persistent, identity: identity}
	store.cleanup(time.Now())
	return store, nil
}

func (s *bootstrapDiskStore) path(key [32]byte) string {
	h := sha256.New()
	h.Write([]byte(bootstrapDiskSchema))
	h.Write([]byte{0})
	h.Write([]byte(s.identity))
	h.Write([]byte{0})
	h.Write(key[:])
	return filepath.Join(s.dir, fmt.Sprintf("%s-%x.blob", bootstrapDiskSchema, h.Sum(nil)))
}

func (s *bootstrapDiskStore) load(key [32]byte) (engine.BootstrapSnapshot, error) {
	path := s.path(key)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	bad := func(err error) (engine.BootstrapSnapshot, error) { _ = os.Remove(path); return nil, err }
	if len(data) < bootstrapDiskHeader || string(data[:16]) != string(bootstrapDiskMagic[:]) {
		return bad(errors.New("invalid bootstrap cache header"))
	}
	size := binary.LittleEndian.Uint64(data[48:56])
	payload := data[56:]
	if size != uint64(len(payload)) || sha256.Sum256(payload) != [32]byte(data[16:48]) {
		return bad(errors.New("invalid bootstrap cache checksum"))
	}
	snapshot, err := s.factory.LoadBootstrapSnapshot(payload)
	if err != nil {
		return bad(err)
	}
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	return snapshot, nil
}

func (s *bootstrapDiskStore) save(key [32]byte, snapshot engine.BootstrapSnapshot) error {
	persistent, ok := snapshot.(engine.PersistentBootstrapSnapshot)
	if !ok {
		return nil
	}
	payload := persistent.BootstrapSnapshotBytes()
	if len(payload) == 0 {
		return errors.New("empty bootstrap snapshot")
	}
	data := make([]byte, bootstrapDiskHeader+len(payload))
	copy(data[:16], bootstrapDiskMagic[:])
	sum := sha256.Sum256(payload)
	copy(data[16:48], sum[:])
	binary.LittleEndian.PutUint64(data[48:56], uint64(len(payload)))
	copy(data[56:], payload)
	path := s.path(key)
	tmp, err := os.CreateTemp(s.dir, ".mimic-bootstrap-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return nil
		}
		return err
	}
	s.cleanup(time.Now())
	return nil
}

func (s *bootstrapDiskStore) cleanup(now time.Time) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	type candidate struct {
		path     string
		modified time.Time
	}
	var files []candidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), bootstrapDiskSchema+"-") || !strings.HasSuffix(entry.Name(), ".blob") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(s.dir, entry.Name())
		if now.Sub(info.ModTime()) > bootstrapDiskMaxAge {
			_ = os.Remove(path)
			continue
		}
		files = append(files, candidate{path, info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modified.After(files[j].modified) })
	if len(files) > bootstrapDiskKeep {
		for _, file := range files[bootstrapDiskKeep:] {
			_ = os.Remove(file.path)
		}
	}
}

// ConfigureBootstrapCache opens the persistent store without creating a Page.
// The first Page can use an existing artifact while cold-cache preparation runs
// independently of protocol listener startup.
func (b *Browser) ConfigureBootstrapCache(dir string) error {
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") == "1" {
		return nil
	}
	b.bootstrapSnapshots.mu.Lock()
	if b.bootstrapSnapshots.disk != nil {
		b.bootstrapSnapshots.mu.Unlock()
		return nil
	}
	b.bootstrapSnapshots.mu.Unlock()
	store, err := newBootstrapDiskStore(dir, b.factory)
	if err != nil || store == nil {
		return err
	}
	b.bootstrapSnapshots.mu.Lock()
	b.bootstrapSnapshots.disk = store
	b.bootstrapSnapshots.mu.Unlock()
	return nil
}

// PrepareBootstrap ensures the default profile's immutable startup artifact is
// available. Callers may run it after opening a protocol listener.
func (b *Browser) PrepareBootstrap(ctx context.Context, dir string) error {
	if dir != "" {
		if err := b.ConfigureBootstrapCache(dir); err != nil {
			return err
		}
	}
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") == "1" {
		return nil
	}
	c, err := b.newPrivateContext()
	if err != nil {
		return err
	}
	defer c.finishPrivateWork()
	for i := 0; i < 2; i++ {
		p, err := c.NewPage()
		if err != nil {
			return err
		}
		if _, err = p.Evaluate(ctx, "true"); err != nil {
			_ = c.ClosePage(p.ID)
			return err
		}
		key := p.Top.Realm.bootstrapSource().key
		if !c.ClosePage(p.ID) {
			return errors.New("close bootstrap preparation page")
		}
		// A disk hit is admitted while constructing the first realm. Only a cold
		// cache needs the second realm which turns the captured seed into a blob.
		if b.bootstrapSnapshots.hasSnapshotKey(key) {
			return nil
		}
	}
	return b.bootstrapSnapshots.wait(ctx)
}
