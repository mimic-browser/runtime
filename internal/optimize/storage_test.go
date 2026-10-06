package optimize

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedCleanupPreservesActiveAndUserFiles(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 25; i++ {
		path := filepath.Join(root, fmt.Sprintf("run-%02d", i))
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	active := filepath.Join(root, "run-active")
	os.Mkdir(active, 0700)
	os.WriteFile(filepath.Join(active, ".active"), []byte(fmt.Sprint(os.Getpid())), 0600)
	user := filepath.Join(root, "user.mcap")
	os.WriteFile(user, []byte("preserved"), 0600)
	if err := pruneManaged(root, ""); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{active, user} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unowned/active evidence removed: %s", path)
		}
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 22 {
		t.Fatalf("unexpected retained entries: %d", len(entries))
	}
}
