//go:build windows

package recovery

// Windows does not expose a portable directory fsync through the Go standard
// library. File data is flushed before publication; directory-entry durability
// remains best effort on this platform.
func syncDir(path string) error { return nil }
