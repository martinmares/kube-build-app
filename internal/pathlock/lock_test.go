package pathlock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingPathUnderSymlinkKeepsSameLockAfterCreation(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	alias := filepath.Join(root, "alias")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	unlock, err := NewLockManager().TryAcquire(filepath.Join(alias, "new-cache"), "create")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := os.Mkdir(filepath.Join(real, "new-cache"), 0700); err != nil {
		t.Fatal(err)
	}
	if release, err := NewLockManager().TryAcquire(filepath.Join(real, "new-cache"), "read"); err == nil {
		release()
		t.Fatal("creation changed the lock key")
	}
}
