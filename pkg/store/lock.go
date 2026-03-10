package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

const lockPath = ".lock"

var processLock sync.Mutex

type Lock struct {
	file *os.File
}

// Lock serializes store mutations across goroutines and processes.
func (s Store) Lock() (*Lock, error) {
	lock, err := acquireLock(s.Root)
	if err != nil {
		return nil, err
	}
	return lock, nil
}

func acquireLock(root string) (*Lock, error) {
	cleanRoot := filepath.Clean(root)

	processLock.Lock()

	if err := os.MkdirAll(cleanRoot, 0o755); err != nil {
		processLock.Unlock()
		return nil, fmt.Errorf("create store root %s for lock: %w", cleanRoot, err)
	}

	file, err := os.OpenFile(filepath.Join(cleanRoot, lockPath), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		processLock.Unlock()
		return nil, fmt.Errorf("open lock in %s: %w", cleanRoot, err)
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		processLock.Unlock()
		_ = file.Close()
		return nil, fmt.Errorf("lock %s: %w", cleanRoot, err)
	}

	return &Lock{file: file}, nil
}

func (l *Lock) Unlock() error {
	if l == nil || l.file == nil {
		return nil
	}

	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	processLock.Unlock()

	if unlockErr != nil {
		return fmt.Errorf("unlock file lock: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close lock file: %w", closeErr)
	}
	return nil
}
