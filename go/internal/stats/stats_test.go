package stats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTokensRoundsUpAfterSumming(t *testing.T) {
	if Tokens(0) != 0 || Tokens(1) != 1 || Tokens(4) != 1 || Tokens(5) != 2 {
		t.Fatal("token ceiling wrong")
	}
}

func TestWriteAndGain(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	events := []Event{
		{ID: "a", StartedAtUTC: base, CommandFamily: "git status", ParserID: "git-status/v1", Mode: "filtered", RawStdoutBytes: 400, RawStderrBytes: 0, EmittedStdoutBytes: 100, EmittedStderrBytes: 0, RecoveryAvailable: true},
		{ID: "b", StartedAtUTC: base.Add(time.Minute), CommandFamily: "rg", ParserID: "rg/v1", Mode: "fallback", ExitCode: 2, RawStdoutBytes: 10, RawStderrBytes: 10, EmittedStdoutBytes: 10, EmittedStderrBytes: 10, FallbackReason: "ambiguous"},
		{ID: "c", StartedAtUTC: base.Add(2 * time.Minute), CommandFamily: "go test", Mode: "raw", RawStdoutBytes: 7, EmittedStdoutBytes: 7},
		{ID: "d", StartedAtUTC: base.Add(3 * time.Minute), Mode: "recall", EmittedStdoutBytes: 50},
	}
	for _, e := range events {
		if err := Write(root, e); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// A fresh recovery manifest makes the historical flag currently readable.
	id := strings.Repeat("a", 32)
	runDir := filepath.Join(root, "runs", id)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"complete":true,"expires_at":"2030-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(runDir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, Event{ID: id, StartedAtUTC: base, Mode: "filtered", RawStdoutBytes: 10, EmittedStdoutBytes: 5, RecoveryAvailable: true}); err != nil {
		t.Fatal(err)
	}

	// Corrupt event must be reported, not fatal.
	corruptDir := filepath.Join(root, "stats", base.Format("2006-01-02"))
	if err := os.WriteFile(filepath.Join(corruptDir, "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	sum, err := Gain(root, base.Add(-time.Hour), base.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Runs != 4 || sum.Filtered != 2 || sum.Fallback != 1 || sum.RawRuns != 1 {
		t.Fatalf("counts = %+v", sum)
	}
	if sum.RecoveryAvailable != 2 || sum.RecoveryAvailableNow != 1 {
		t.Fatalf("recovery = %d/%d", sum.RecoveryAvailableNow, sum.RecoveryAvailable)
	}
	if sum.Failed != 1 || sum.CorruptEvents != 1 {
		t.Fatalf("failed/corrupt = %d/%d", sum.Failed, sum.CorruptEvents)
	}
	if sum.RawStdoutBytes != 427 || sum.RawStderrBytes != 10 {
		t.Fatalf("raw bytes = %d/%d", sum.RawStdoutBytes, sum.RawStderrBytes)
	}
	if sum.EmittedStdoutBytes != 122 || sum.EmittedStderrBytes != 10 {
		t.Fatalf("emitted bytes = %d/%d", sum.EmittedStdoutBytes, sum.EmittedStderrBytes)
	}
	if sum.EstimatedInputTokens != Tokens(437) || sum.EstimatedOutputTokens != Tokens(132) {
		t.Fatalf("tokens = %d/%d", sum.EstimatedInputTokens, sum.EstimatedOutputTokens)
	}
	if sum.RecallCount != 1 || sum.RecallBytes != 50 {
		t.Fatalf("recall = %d/%d", sum.RecallCount, sum.RecallBytes)
	}
	if sum.GrossSavedBytes != 305 || sum.NetSavedBytes != 255 {
		t.Fatalf("savings = %d/%d", sum.GrossSavedBytes, sum.NetSavedBytes)
	}
}

func TestGainNoData(t *testing.T) {
	sum, err := Gain(t.TempDir(), time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Runs != 0 || sum.GrossSavedBytes != 0 || sum.EstimatedInputTokens != 0 {
		t.Fatalf("no-data summary = %+v", sum)
	}
}

func TestParseSince(t *testing.T) {
	cases := map[string]time.Duration{
		"":    7 * 24 * time.Hour,
		"7d":  7 * 24 * time.Hour,
		"24h": 24 * time.Hour,
		"30m": 30 * time.Minute,
		"60s": time.Minute,
	}
	for in, want := range cases {
		got, err := ParseSince(in)
		if err != nil || got != want {
			t.Errorf("ParseSince(%q) = %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"d", "0d", "7x", "-1d", "1.5h"} {
		if _, err := ParseSince(bad); err == nil {
			t.Errorf("ParseSince(%q) should fail", bad)
		}
	}
}

func TestCleanRetention(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := Event{ID: "old", StartedAtUTC: now.Add(-40 * 24 * time.Hour), Mode: "raw"}
	fresh := Event{ID: "fresh", StartedAtUTC: now, Mode: "raw"}
	if err := Write(root, old); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, fresh); err != nil {
		t.Fatal(err)
	}
	// Write sets mtime to now; age the old file explicitly.
	oldPath := filepath.Join(root, "stats", old.StartedAtUTC.Format("2006-01-02"), "old.json")
	aged := now.Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(oldPath, aged, aged); err != nil {
		t.Fatal(err)
	}
	count, _, err := Clean(root, 30, now, true)
	if err != nil || count != 1 {
		t.Fatalf("dry run count=%d err=%v", count, err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatal("dry run removed a file")
	}
	count, _, err = Clean(root, 30, now, false)
	if err != nil || count != 1 {
		t.Fatalf("clean count=%d err=%v", count, err)
	}
	if _, err := os.Stat(oldPath); err == nil {
		t.Fatal("old event survived cleanup")
	}
	freshPath := filepath.Join(root, "stats", fresh.StartedAtUTC.Format("2006-01-02"), "fresh.json")
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatal("fresh event removed")
	}
}
