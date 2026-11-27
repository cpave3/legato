package main

import (
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

// claimDataDirLock takes the lock file name in dataDir without waiting, so
// only one process on a data directory plays a role. The release it
// returns may be called more than once. It keeps the file in place:
// removing it on unlock could let two processes lock different files at
// one path.
func claimDataDirLock(dataDir, name string) (func(), error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dataDir, name), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); _ = file.Close() }) }, nil
}
