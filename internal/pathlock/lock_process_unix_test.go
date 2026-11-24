//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package pathlock

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestProcessLockReleaseWithDuplicatedDescriptor(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "repository.lock")
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	duplicate, err := syscall.Dup(int(file.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(duplicate)
	releaseProcessLock(file)
	release, err := acquireProcessLock(filename)
	if err != nil {
		t.Fatalf("released lock still held through duplicated descriptor: %v", err)
	}
	release()
}
