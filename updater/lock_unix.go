//go:build darwin || linux

package updater

import (
	"os"

	"golang.org/x/sys/unix"
)

func acquireLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	// Keep the lock file in place: unlinking it would allow two processes to
	// lock different inodes. The OS releases the lock even if the updater crashes.
	return func() { _ = f.Close() }, nil
}
