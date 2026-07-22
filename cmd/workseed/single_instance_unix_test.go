//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"path/filepath"
	"testing"
)

func TestAcquireFileInstanceLockPreventsSecondInstance(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "workseed.lock")

	releaseFirst, acquired, err := acquireFileInstanceLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("expected first instance to acquire lock")
	}

	releaseSecond, acquired, err := acquireFileInstanceLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		releaseSecond()
		t.Fatal("expected second instance to be rejected")
	}

	releaseFirst()

	releaseThird, acquired, err := acquireFileInstanceLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("expected lock to be available after release")
	}
	releaseThird()
}
