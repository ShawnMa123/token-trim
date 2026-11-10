package recovery

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return newTestStoreWith(t, 256)
}

func newTestStoreWith(t *testing.T, totalMiB int) *Store {
	t.Helper()
	s := New(filepath.Join(t.TempDir(), "data"), totalMiB, 24)
	if err := s.Prepare(); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	return s
}

func commitOne(t *testing.T, s *Store, stdout, stderr string) string {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	res, ok := s.Acquire(id)
	if !ok {
		t.Fatal("acquire failed")
	}
	if _, err := res.Commit(id, []byte(stdout), []byte(stderr), 0, "utf-8"); err != nil {
		res.Release()
		t.Fatalf("commit: %v", err)
	}
	res.Release()
	return id
}

func TestCommitLoadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	id := commitOne(t, s, "out\x00put", "warn\n")
	rec, err := s.Load(id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(rec.Stdout) != "out\x00put" || string(rec.Stderr) != "warn\n" {
		t.Fatalf("streams = %q / %q", rec.Stdout, rec.Stderr)
	}
	if !rec.Complete || rec.ID != id || rec.Encoding != "utf-8" {
		t.Fatalf("manifest = %+v", rec.Manifest)
	}
}

func TestLoadRejectsTraversalAndMissing(t *testing.T) {
	s := newTestStore(t)
	for _, id := range []string{"", "../x", strings.Repeat("a", 31), strings.Repeat("A", 32), "/abs", strings.Repeat("a", 32) + "/"} {
		if _, err := s.Load(id); err == nil {
			t.Errorf("Load(%q) succeeded", id)
		}
	}
	if _, err := s.Load(strings.Repeat("b", 32)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing record err = %v", err)
	}
}

func TestDigestMismatchIsCorrupt(t *testing.T) {
	s := newTestStore(t)
	id := commitOne(t, s, "original", "")
	p := filepath.Join(s.runsDir(), id, "stdout.bin")
	if err := os.WriteFile(p, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v", err)
	}
}

func TestExpiredRecord(t *testing.T) {
	s := newTestStore(t)
	base := time.Now()
	s.Now = func() time.Time { return base }
	id := commitOne(t, s, "x", "")
	if _, err := s.Load(id); err != nil {
		t.Fatalf("fresh load: %v", err)
	}
	s.Now = func() time.Time { return base.Add(25 * time.Hour) }
	if _, err := s.Load(id); !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v", err)
	}
}

func TestPartialIsNeverRecoverable(t *testing.T) {
	s := newTestStore(t)
	id, _ := NewID()
	partial := filepath.Join(s.Root, ".partial-test")
	if err := os.MkdirAll(partial, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(partial, "stdout.bin"), []byte("x"), 0o600)
	if _, err := os.Stat(filepath.Join(s.runsDir(), id)); err == nil {
		t.Fatal("unexpected committed record")
	}
	if _, err := s.Load(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestQuotaReservation(t *testing.T) {
	s := newTestStoreWith(t, 64)
	id1, _ := NewID()
	r1, ok := s.Acquire(id1)
	if !ok {
		t.Fatal("first acquire failed")
	}
	id2, _ := NewID()
	if _, ok := s.Acquire(id2); ok {
		t.Fatal("second acquire must fail while 64 MiB is reserved")
	}
	r1.Release()
	if _, ok := s.Acquire(id2); !ok {
		t.Fatal("acquire after release failed")
	}
}

func TestLockContentionDegradesSafely(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(s.lockPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	id, _ := NewID()
	if _, ok := s.Acquire(id); ok {
		t.Fatal("acquire must fail while the lock is held")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("lock wait took %v", elapsed)
	}
}

func TestCleanExpiredOnly(t *testing.T) {
	s := newTestStore(t)
	base := time.Now()
	s.Now = func() time.Time { return base }
	expired := commitOne(t, s, "old", "")

	s.Now = func() time.Time { return base.Add(12 * time.Hour) }
	valid := commitOne(t, s, "new", "")

	s.Now = func() time.Time { return base.Add(25 * time.Hour) }
	entries, err := s.CleanExpired(true, base.Add(25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != expired {
		t.Fatalf("dry run entries = %+v", entries)
	}
	if _, err := os.Stat(filepath.Join(s.runsDir(), expired)); err != nil {
		t.Fatal("dry run removed data")
	}
	if _, err := s.CleanExpired(false, base.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.runsDir(), expired)); err == nil {
		t.Fatalf("expired record %s survived cleanup", expired)
	}
	if _, err := s.Load(valid); err != nil {
		t.Fatalf("valid record removed: %v", err)
	}
}

func TestValidRecordSurvivesCleanup(t *testing.T) {
	s := newTestStore(t)
	base := time.Now()
	s.Now = func() time.Time { return base }
	id := commitOne(t, s, "data", "")
	if _, err := s.CleanExpired(false, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(id); err != nil {
		t.Fatalf("valid record removed: %v", err)
	}
}

func TestSymlinkRunDirRejected(t *testing.T) {
	s := newTestStore(t)
	id := commitOne(t, s, "data", "")
	dir := filepath.Join(s.runsDir(), id)
	moved := filepath.Join(s.Root, "moved-"+id)
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, dir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := s.Load(id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.CleanExpired(false, time.Now().Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(moved); err != nil {
		t.Fatal("cleanup followed a symlink and removed the target")
	}
}
