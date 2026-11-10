package recovery

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// ErrNotText reports that a stream cannot be displayed as UTF-8 text.
var ErrNotText = errors.New("stream is not valid UTF-8 text; use a single stream with --raw")

// Handle is a verified, open recovery record. It never re-executes anything.
type Handle struct {
	Manifest
	dir string
}

// Open verifies the manifest, expiry, sizes, and digests without loading the
// streams into memory.
func (s *Store) Open(id string) (*Handle, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("invalid recovery id")
	}
	dir, err := s.containedRunDir(id)
	if err != nil {
		return nil, err
	}
	mb, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var m Manifest
	if err := jsonUnmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("%w: manifest", ErrCorrupt)
	}
	if m.Schema != Schema || m.ID != id || !m.Complete {
		return nil, fmt.Errorf("%w: incomplete or unsupported record", ErrCorrupt)
	}
	if s.now().After(m.ExpiresAt) {
		return nil, ErrExpired
	}
	if err := verifyFileDigest(filepath.Join(dir, "stdout.bin"), m.StdoutBytes, m.StdoutSHA256); err != nil {
		return nil, err
	}
	if m.StderrBytes > 0 {
		if err := verifyFileDigest(filepath.Join(dir, "stderr.bin"), m.StderrBytes, m.StderrSHA256); err != nil {
			return nil, err
		}
	}
	return &Handle{Manifest: m, dir: dir}, nil
}

func jsonUnmarshal(b []byte, v any) error {
	return json.Unmarshal(b, v)
}

func verifyFileDigest(path string, size int64, want string) error {
	if size == 0 {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("%w: unexpected stream file", ErrCorrupt)
		}
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: stream is not a regular file", ErrCorrupt)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil || n != size || hex.EncodeToString(h.Sum(nil)) != want {
		return fmt.Errorf("%w: digest mismatch", ErrCorrupt)
	}
	return nil
}

func (h *Handle) streamPath(stream string) (string, int64) {
	if stream == "stderr" {
		return filepath.Join(h.dir, "stderr.bin"), h.StderrBytes
	}
	return filepath.Join(h.dir, "stdout.bin"), h.StdoutBytes
}

// Raw copies one stream verbatim with no headers or newline translation.
func (h *Handle) Raw(stream string, w io.Writer) (int64, error) {
	path, size := h.streamPath(stream)
	if size == 0 {
		return 0, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(w, f)
}

// Text writes a header and a 1-based line range. It streams the file twice
// (line count, then page) and never loads the whole stream.
func (h *Handle) Text(stream string, w io.Writer, offset, limit int) (int64, error) {
	path, size := h.streamPath(stream)
	if size == 0 {
		return writeString(w, fmt.Sprintf("[%s] (empty)\n", stream))
	}
	if err := validateTextFile(path); err != nil {
		return 0, err
	}
	total, err := countLines(path)
	if err != nil {
		return 0, err
	}
	if total == 0 {
		return writeString(w, fmt.Sprintf("[%s] (no lines)\n", stream))
	}
	if offset > total {
		return writeString(w, fmt.Sprintf("[%s] no lines at offset %d (total %d)\n", stream, offset, total))
	}
	end := offset + limit - 1
	if end > total {
		end = total
	}
	var written int64
	header := fmt.Sprintf("[%s] lines %d-%d of %d", stream, offset, end, total)
	if end < total {
		header += fmt.Sprintf(" (next offset %d)", end+1)
	}
	header += "\n"
	n, err := writeString(w, header)
	written += n
	if err != nil {
		return written, err
	}
	n, err = copyLineRange(path, w, offset, end)
	written += n
	return written, err
}

func writeString(w io.Writer, s string) (int64, error) {
	n, err := io.WriteString(w, s)
	return int64(n), err
}

func validateTextFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	for {
		r, size, err := br.ReadRune()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if r == utf8.RuneError && size == 1 {
			return ErrNotText
		}
	}
}

func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	total := 0
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			total++
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return 0, err
		}
	}
}

func copyLineRange(path string, w io.Writer, start, end int) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	var written int64
	lineNo := 0
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			lineNo++
			if lineNo >= start && lineNo <= end {
				line = trimEOL(line)
				n, werr := io.WriteString(w, line+"\n")
				written += int64(n)
				if werr != nil {
					return written, werr
				}
			}
		}
		if err == io.EOF {
			return written, nil
		}
		if err != nil {
			return written, err
		}
	}
}

func trimEOL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
