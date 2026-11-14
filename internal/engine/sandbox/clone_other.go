//go:build !darwin

package sandbox

import "errors"

// CloneTree is APFS's clonefile, which only macOS has.
func CloneTree(src, dst string) error {
	return errors.New("copy-on-write clones of a folder need macOS (APFS)")
}
