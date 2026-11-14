package sandbox

import "golang.org/x/sys/unix"

// CloneTree makes dst a copy-on-write clone of the file or folder src
// (clonefile(2)): instant on APFS, sharing blocks until either side
// changes them. dst must not exist, and must be on src's volume.
func CloneTree(src, dst string) error {
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW|unix.CLONE_NOOWNERCOPY)
}
