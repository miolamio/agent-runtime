//go:build !windows

package users

import (
	"os"
	"syscall"
)

// Lock a stable sidecar inode: users.json itself is replaced by rename.
// Closing the descriptor releases the kernel lock even after a process crash.
func lockStore(path string) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
