//go:build !windows

package recovery

import "os"

// syncDir makes directory entries durable. It is used after atomic renames so
// a committed record is not lost by a crash immediately after publication.
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
