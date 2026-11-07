package tmux

import (
	"bytes"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPTY opens a new pseudo-terminal: /dev/ptmx, then the ioctls macOS's
// grantpt, unlockpt and ptsname make, and opens the slave by that name.
func openPTY() (master, slave *os.File, err error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	master = os.NewFile(uintptr(fd), "/dev/ptmx")
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		master.Close()
		return nil, nil, err
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		master.Close()
		return nil, nil, err
	}
	// TIOCPTYGNAME writes the slave's path, NUL-terminated, into 128 bytes.
	var name [128]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		master.Close()
		return nil, nil, errno
	}
	end := bytes.IndexByte(name[:], 0)
	if end < 0 {
		end = len(name)
	}
	path := string(name[:end])
	slave, err = os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}
