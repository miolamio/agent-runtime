package users

import (
	"os"
	"syscall"
	"unsafe"
)

var lockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

func lockStore(path string) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	var overlapped syscall.Overlapped
	result, _, callErr := lockFileEx.Call(f.Fd(), 2, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if result == 0 {
		_ = f.Close()
		return nil, callErr
	}
	return func() { _ = f.Close() }, nil
}
