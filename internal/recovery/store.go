// Package recovery stores complete original streams and exposes paginated
// recall. Lossy output is never published before a complete record commits.
package recovery

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Schema is the recovery record schema version.
const Schema = 1

// ReserveBytes is the fixed per-invocation reservation, covering the maximum
// stdout (32 MiB) + stderr (31 MiB) + manifest overhead.
const ReserveBytes = 64 << 20

var (
	ErrDisabled = errors.New("recovery storage is disabled")
	ErrNotFound = errors.New("recovery record not found")
	ErrExpired  = errors.New("recovery record expired")
	ErrCorrupt  = errors.New("recovery record is corrupt")
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Manifest is the committed record metadata.
type Manifest struct {
	Schema       int       `json:"schema"`
	ID           string    `json:"id"`
	StdoutBytes  int64     `json:"stdout_bytes"`
	StderrBytes  int64     `json:"stderr_bytes"`
	StdoutSHA256 string    `json:"stdout_sha256"`
	StderrSHA256 string    `json:"stderr_sha256"`
	Encoding     string    `json:"encoding"`
	ExitCode     int       `json:"exit_code"`
	Complete     bool      `json:"complete"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Store owns one private data root.
type Store struct {
	Root        string
	TotalBytes  int64
	Retention   time.Duration
	LockTimeout time.Duration
	Now         func() time.Time
}

// New builds a store. Callers must run Prepare (or Acquire) before writing.
func New(root string, totalMiB, retentionHours int) *Store {
	return &Store{
		Root:        root,
		TotalBytes:  int64(totalMiB) << 20,
		Retention:   time.Duration(retentionHours) * time.Hour,
		LockTimeout: 200 * time.Millisecond,
		Now:         time.Now,
	}
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Prepare creates the private data root and checks isolation.
func (s *Store) Prepare() error {
	if s.Root == "" {
		return ErrDisabled
	}
	return secureDir(s.Root)
}

// Check verifies an existing data root without creating or modifying it.
// It returns false when the root does not exist yet.
func (s *Store) Check() (bool, error) {
	if s.Root == "" {
		return false, nil
	}
	info, err := os.Lstat(s.Root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true, fmt.Errorf("data root is a link")
	}
	if !info.IsDir() {
		return true, fmt.Errorf("data root is not a directory")
	}
	return true, checkPrivateDir(s.Root)
}

// NewID returns a fresh 128-bit invocation id.
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// ValidID reports whether id is a normalized 32-hex identifier.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Reservation is a held quota lease. All methods are safe for concurrent use.
type Reservation struct {
	store *Store
	runID string
	path  string
	token string
	size  int64
	stop  chan struct{}
	once  sync.Once
	wg    sync.WaitGroup
}

func (s *Store) runsDir() string     { return filepath.Join(s.Root, "runs") }
func (s *Store) statsDir() string    { return filepath.Join(s.Root, "stats") }
func (s *Store) leasesDir() string   { return filepath.Join(s.Root, "leases") }
func (s *Store) lockPath() string    { return filepath.Join(s.Root, "locks", "quota.lock") }
func (s *Store) partialRoot() string { return s.Root }

// Acquire reserves quota. It returns false when storage is unavailable, the
// lock is contended beyond 200ms, or the budget would be exceeded.
func (s *Store) Acquire(id string) (*Reservation, bool) {
	if err := s.Prepare(); err != nil {
		return nil, false
	}
	unlock, ok := s.lock()
	if !ok {
		return nil, false
	}
	defer unlock()

	s.purgeStaleLeases()
	usage, err := s.usage()
	if err != nil {
		return nil, false
	}
	if usage+ReserveBytes > s.TotalBytes {
		return nil, false
	}
	if err := os.MkdirAll(s.leasesDir(), 0o700); err != nil {
		return nil, false
	}
	token, err := NewID()
	if err != nil {
		return nil, false
	}
	lease := leaseFile{
		ID:        id,
		Token:     token,
		Bytes:     ReserveBytes,
		PID:       os.Getpid(),
		ExpiresAt: s.now().Add(30 * time.Second),
	}
	path := filepath.Join(s.leasesDir(), token+".json")
	if err := writeJSONAtomic(path, lease); err != nil {
		return nil, false
	}
	r := &Reservation{store: s, runID: id, path: path, token: token, size: ReserveBytes, stop: make(chan struct{})}
	r.wg.Add(1)
	go r.refresh()
	return r, true
}

func (r *Reservation) refresh() {
	defer r.wg.Done()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
			lease := leaseFile{
				ID:        r.runID,
				Token:     r.token,
				Bytes:     r.size,
				PID:       os.Getpid(),
				ExpiresAt: r.store.now().Add(30 * time.Second),
			}
			_ = writeJSONAtomic(r.path, lease)
		}
	}
}

// Release drops the lease. It is idempotent.
func (r *Reservation) Release() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		close(r.stop)
		r.wg.Wait()
		_ = os.Remove(r.path)
	})
}

// Commit atomically publishes the complete original streams.
func (r *Reservation) Commit(id string, stdout, stderr []byte, exitCode int, encoding string) (Manifest, error) {
	if r == nil {
		return Manifest{}, ErrDisabled
	}
	if r.runID != "" && id != r.runID {
		return Manifest{}, fmt.Errorf("recovery id does not match the reservation")
	}
	s := r.store
	if !ValidID(id) {
		return Manifest{}, fmt.Errorf("invalid recovery id")
	}
	if err := os.MkdirAll(s.runsDir(), 0o700); err != nil {
		return Manifest{}, err
	}
	partial, err := os.MkdirTemp(s.partialRoot(), ".partial-")
	if err != nil {
		return Manifest{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(partial)
		}
	}()

	now := s.now()
	m := Manifest{
		Schema:       Schema,
		ID:           id,
		StdoutBytes:  int64(len(stdout)),
		StderrBytes:  int64(len(stderr)),
		StdoutSHA256: digest(stdout),
		StderrSHA256: digest(stderr),
		Encoding:     encoding,
		ExitCode:     exitCode,
		Complete:     true,
		CreatedAt:    now.UTC(),
		ExpiresAt:    now.Add(s.Retention).UTC(),
	}
	if err := writeFileSync(filepath.Join(partial, "stdout.bin"), stdout); err != nil {
		return Manifest{}, err
	}
	if len(stderr) > 0 {
		if err := writeFileSync(filepath.Join(partial, "stderr.bin"), stderr); err != nil {
			return Manifest{}, err
		}
	}
	mb, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	if err := writeFileSync(filepath.Join(partial, "manifest.json"), append(mb, '\n')); err != nil {
		return Manifest{}, err
	}
	if err := syncDir(partial); err != nil {
		return Manifest{}, err
	}
	final := filepath.Join(s.runsDir(), id)
	if err := os.Rename(partial, final); err != nil {
		return Manifest{}, err
	}
	// Persist the new directory entry before reporting the record committed.
	if err := syncDir(s.runsDir()); err != nil {
		return Manifest{}, err
	}
	if err := syncDir(s.Root); err != nil {
		return Manifest{}, err
	}
	committed = true
	return m, nil
}

func (s *Store) containedRunDir(id string) (string, error) {
	rootReal, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	dir := filepath.Join(s.runsDir(), id)
	info, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: run directory is a link", ErrCorrupt)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if !within(rootReal, real) {
		return "", fmt.Errorf("%w: run directory escapes the managed root", ErrCorrupt)
	}
	return dir, nil
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeFileSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return syncDir(dir)
}

// leaseFile is a quota lease owned by one invocation.
type leaseFile struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	Bytes     int64     `json:"bytes"`
	PID       int       `json:"pid"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Store) lock() (func(), bool) {
	if err := os.MkdirAll(filepath.Join(s.Root, "locks"), 0o700); err != nil {
		return nil, false
	}
	timeout := s.LockTimeout
	if timeout <= 0 {
		timeout = 200 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := os.Mkdir(s.lockPath(), 0o700); err == nil {
			return func() { _ = os.Remove(s.lockPath()) }, true
		}
		if time.Now().After(deadline) {
			return nil, false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *Store) purgeStaleLeases() {
	entries, err := os.ReadDir(s.leasesDir())
	if err != nil {
		return
	}
	now := s.now()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(s.leasesDir(), e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var l leaseFile
		if json.Unmarshal(data, &l) != nil {
			continue
		}
		if now.After(l.ExpiresAt) {
			_ = os.Remove(path)
		}
	}
}

func (s *Store) usage() (int64, error) {
	var total int64
	for _, dir := range []string{s.runsDir(), s.statsDir()} {
		n, err := dirSize(dir)
		if err != nil {
			return 0, err
		}
		total += n
	}
	partials, err := filepath.Glob(filepath.Join(s.Root, ".partial-*"))
	if err != nil {
		return 0, err
	}
	for _, p := range partials {
		n, _ := dirSize(p)
		total += n
	}
	// Active reservations count at their reserved size, not their tiny lease
	// metadata. This keeps the 256 MiB budget at four 64 MiB reservations.
	leases, err := os.ReadDir(s.leasesDir())
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return 0, err
		}
		return total, nil
	}
	for _, e := range leases {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.leasesDir(), e.Name()))
		if err != nil {
			continue
		}
		var l leaseFile
		if json.Unmarshal(data, &l) == nil {
			total += l.Bytes
		}
	}
	return total, nil
}

func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// ExpiredEntry describes one cleanup candidate.
type ExpiredEntry struct {
	ID    string
	Bytes int64
	Path  string
}

// CleanExpired removes only expired, non-active managed objects. When dryRun
// is set nothing is written or removed.
func (s *Store) CleanExpired(dryRun bool, now time.Time) ([]ExpiredEntry, error) {
	entries, err := os.ReadDir(s.runsDir())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	rootReal, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		return nil, err
	}
	var out []ExpiredEntry
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		dir := filepath.Join(s.runsDir(), e.Name())
		info, err := os.Lstat(dir)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		real, err := filepath.EvalSymlinks(dir)
		if err != nil || !within(rootReal, real) {
			continue
		}
		mb, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			continue
		}
		var m Manifest
		if json.Unmarshal(mb, &m) != nil || !m.Complete || !now.After(m.ExpiresAt) {
			continue
		}
		size, _ := dirSize(dir)
		out = append(out, ExpiredEntry{ID: e.Name(), Bytes: size, Path: dir})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	partials, _ := filepath.Glob(filepath.Join(s.Root, ".partial-*"))
	for _, p := range partials {
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if now.Sub(info.ModTime()) < 24*time.Hour {
			continue
		}
		size, _ := dirSize(p)
		out = append(out, ExpiredEntry{ID: filepath.Base(p), Bytes: size, Path: p})
	}

	if dryRun {
		return out, nil
	}
	for _, e := range out {
		trash := filepath.Join(s.Root, ".trash-"+e.ID)
		if err := os.Rename(e.Path, trash); err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(trash); err == nil && within(rootReal, real) {
			_ = os.RemoveAll(trash)
		}
	}
	return out, nil
}
