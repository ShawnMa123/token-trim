//go:build !windows

package recovery

import (
	"fmt"
	"os"
)

// checkPrivateDir verifies owner-only permissions without changing them.
func checkPrivateDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("data root is not a directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("data root permissions are not owner-only")
	}
	return nil
}

// secureDir creates path and forces owner-only Unix permissions, regardless of
// a permissive umask.
func secureDir(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("data root is a symlink")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("data root is not a real directory")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("data root is not a directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("data root permissions are not owner-only")
	}
	return nil
}
