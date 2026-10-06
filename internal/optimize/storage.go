package optimize

import (
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func sha256Sum(b []byte) [32]byte { return sha256.Sum256(b) }

func copyAtomic(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.CreateTemp(filepath.Dir(destination), ".evidence-*")
	if err != nil {
		return err
	}
	defer os.Remove(output.Name())
	_, err = io.Copy(output, input)
	if err == nil {
		err = output.Sync()
	}
	if e := output.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(output.Name(), destination)
}

// Prune only completed optimizer-owned entries. Profiles and explicit input
// captures are never removed. Active runs remain owned by their runner.
func pruneManaged(root, keep string) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return err
	}
	type owned struct {
		path     string
		size     int64
		modified time.Time
	}
	var candidates []owned
	var total int64
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "run-") && !(len(name) == 69 && strings.HasSuffix(name, ".mcap")) {
			continue
		}
		path := filepath.Join(absolute, name)
		info, e := os.Lstat(path)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if info.IsDir() {
			if marker, e := os.ReadFile(filepath.Join(path, ".active")); e == nil {
				pid, e := strconv.Atoi(strings.TrimSpace(string(marker)))
				if e != nil || pid <= 0 || processAlive(pid) {
					continue
				}
				_ = os.Remove(filepath.Join(path, ".active"))
			}
		}
		var size int64
		_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, e error) error {
			if e == nil && !d.IsDir() {
				if i, e := d.Info(); e == nil {
					size += i.Size()
				}
			}
			return nil
		})
		total += size
		candidates = append(candidates, owned{path, size, info.ModTime()})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].modified.Before(candidates[j].modified) })
	remaining := len(candidates)
	for _, entry := range candidates {
		if remaining <= 20 && total <= 2<<30 {
			break
		}
		if entry.path == keep {
			continue
		}
		relative, e := filepath.Rel(absolute, entry.path)
		if e != nil || relative == "." || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
			continue
		}
		if e = os.RemoveAll(entry.path); e != nil {
			return e
		}
		remaining--
		total -= entry.size
	}
	return nil
}
