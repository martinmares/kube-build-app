//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package pathlock

import (
	"os"
	"syscall"
)

func acquireProcessLock(filename string) (func(), error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	// Unlock explicitly before closing: a duplicated descriptor in a concurrently
	// spawned child can keep the open file description alive after our close.
	// Leave the inode in place so contenders cannot lock a replacement file.
	return func() { releaseProcessLock(file) }, nil
}

func releaseProcessLock(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}
