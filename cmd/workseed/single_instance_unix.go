//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func acquireSingleInstance() (release func(), acquired bool, err error) {
	lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("workseed-%d.lock", os.Getuid()))
	return acquireFileInstanceLock(lockPath)
}

func acquireFileInstanceLock(lockPath string) (release func(), acquired bool, err error) {
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open lock file: %w", err)
	}

	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lockFile.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock file: %w", err)
	}

	return func() {
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
	}, true, nil
}
