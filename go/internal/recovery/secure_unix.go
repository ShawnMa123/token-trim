//go:build !windows

package recovery

import (
	"fmt"
	"os"
)

// secureDir creates path and forces owner-only Unix permissions, regardless of
// a permissive umask.
func secureDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
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
