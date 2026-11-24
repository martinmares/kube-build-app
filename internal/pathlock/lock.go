package pathlock

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type LockManager struct {
	mu     sync.Mutex
	owners map[string]string
}

func NewLockManager() *LockManager {
	return &LockManager{owners: make(map[string]string)}
}

func (m *LockManager) TryAcquire(path, owner string) (func(), error) {
	canonical, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve lock path: %w", err)
	}
	// A cache may not exist yet, but an ancestor can already be a symlink.
	// Resolve that ancestor so the lock key stays the same after creation.
	for ancestor := canonical; ; ancestor = filepath.Dir(ancestor) {
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err == nil {
			relative, _ := filepath.Rel(ancestor, canonical)
			canonical = filepath.Join(resolved, relative)
			break
		}
		if !os.IsNotExist(err) || filepath.Dir(ancestor) == ancestor {
			return nil, fmt.Errorf("resolve lock path: %w", err)
		}
	}
	canonical = filepath.Clean(canonical)
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, exists := m.owners[canonical]; exists {
		return nil, fmt.Errorf("repository %s is currently used by %s", canonical, current)
	}
	// Web and CLI can be separate processes using the same checkout. Keep the
	// lock outside the repository so Git cleanliness checks remain unchanged.
	key := sha256.Sum256([]byte(canonical))
	directory := filepath.Join(os.TempDir(), fmt.Sprintf("kube-build-app-source-locks-%d", os.Getuid()))
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, fmt.Errorf("create repository lock directory: %w", err)
	}
	releaseProcess, err := acquireProcessLock(filepath.Join(directory, fmt.Sprintf("%x.lock", key)))
	if err != nil {
		return nil, fmt.Errorf("repository %s cannot acquire repository lock (another operation may still hold it): %w", canonical, err)
	}
	m.owners[canonical] = owner
	once := sync.Once{}
	return func() {
		once.Do(func() {
			m.mu.Lock()
			releaseProcess()
			delete(m.owners, canonical)
			m.mu.Unlock()
		})
	}, nil
}
