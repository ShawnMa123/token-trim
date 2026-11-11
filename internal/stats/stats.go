// Package stats writes minimal per-invocation events and aggregates gain.
package stats

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SchemaVersion is the event schema version.
const SchemaVersion = 1

// EstimateMethod labels the byte-based token estimate.
const EstimateMethod = "bytes-div4-v1"

// Event is one invocation record. It never contains argv, paths, environment,
// output bytes, or session data.
type Event struct {
	SchemaVersion         int       `json:"schema_version"`
	ID                    string    `json:"id"`
	StartedAtUTC          time.Time `json:"started_at_utc"`
	CommandFamily         string    `json:"command_family"`
	ParserID              string    `json:"parser_id"`
	Mode                  string    `json:"mode"`
	ExitCode              int       `json:"exit_code"`
	DurationMS            int64     `json:"duration_ms"`
	WrapperOverheadMS     int64     `json:"wrapper_overhead_ms"`
	RawStdoutBytes        int64     `json:"raw_stdout_bytes"`
	RawStderrBytes        int64     `json:"raw_stderr_bytes"`
	EmittedStdoutBytes    int64     `json:"emitted_stdout_bytes"`
	EmittedStderrBytes    int64     `json:"emitted_stderr_bytes"`
	EstimatedInputTokens  int64     `json:"estimated_input_tokens"`
	EstimatedOutputTokens int64     `json:"estimated_output_tokens"`
	EstimateMethod        string    `json:"estimate_method"`
	FallbackReason        string    `json:"fallback_reason,omitempty"`
	RecoveryAvailable     bool      `json:"recovery_available"`
}

// Tokens returns ceil(bytes/4); zero bytes stays zero.
func Tokens(bytes int64) int64 {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 3) / 4
}

func (e *Event) finalize() {
	e.SchemaVersion = SchemaVersion
	e.EstimateMethod = EstimateMethod
	e.EstimatedInputTokens = Tokens(e.RawStdoutBytes + e.RawStderrBytes)
	e.EstimatedOutputTokens = Tokens(e.EmittedStdoutBytes + e.EmittedStderrBytes)
}

// Write commits one event atomically under stats/YYYY-MM-DD/<id>.json.
func Write(root string, e Event) error {
	if e.ID == "" {
		return errors.New("stats: empty event id")
	}
	e.finalize()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if len(data) > 16*1024 {
		return errors.New("stats: event exceeds 16 KiB")
	}
	dir := filepath.Join(root, "stats", e.StartedAtUTC.UTC().Format("2006-01-02"))
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, e.ID+".json"))
}

// Summary aggregates a time range.
type Summary struct {
	Since                 time.Time `json:"since"`
	Until                 time.Time `json:"until"`
	Runs                  int       `json:"runs"`
	Filtered              int       `json:"filtered"`
	Passthrough           int       `json:"passthrough"`
	Fallback              int       `json:"fallback"`
	RawRuns               int       `json:"raw_runs"`
	Failed                int       `json:"failed"`
	RawStdoutBytes        int64     `json:"raw_stdout_bytes"`
	RawStderrBytes        int64     `json:"raw_stderr_bytes"`
	EmittedStdoutBytes    int64     `json:"emitted_stdout_bytes"`
	EmittedStderrBytes    int64     `json:"emitted_stderr_bytes"`
	EstimatedInputTokens  int64     `json:"estimated_input_tokens"`
	EstimatedOutputTokens int64     `json:"estimated_output_tokens"`
	GrossSavedBytes       int64     `json:"gross_saved_bytes"`
	RecoveryAvailable     int       `json:"recovery_available"`
	RecoveryAvailableNow  int       `json:"recovery_available_now"`
	RecallCount           int       `json:"recall_count"`
	RecallBytes           int64     `json:"recall_bytes"`
	NetSavedBytes         int64     `json:"net_saved_bytes"`
	CorruptEvents         int       `json:"corrupt_events"`
	EstimateMethod        string    `json:"estimate_method"`
}

// Gain scans events since the cutoff without modifying them.
func Gain(root string, since, until time.Time) (Summary, error) {
	sum := Summary{Since: since.UTC(), Until: until.UTC(), EstimateMethod: EstimateMethod}
	base := filepath.Join(root, "stats")
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return sum, nil
		}
		return sum, err
	}
	var rawTotal, emittedTotal int64
	for _, day := range entries {
		if !day.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(base, day.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(base, day.Name(), f.Name()))
			if err != nil {
				sum.CorruptEvents++
				continue
			}
			var e Event
			if json.Unmarshal(data, &e) != nil || e.SchemaVersion != SchemaVersion {
				sum.CorruptEvents++
				continue
			}
			if e.StartedAtUTC.Before(since) || e.StartedAtUTC.After(until) {
				continue
			}
			if e.Mode == "recall" {
				sum.RecallCount++
				sum.RecallBytes += e.EmittedStdoutBytes + e.EmittedStderrBytes
				continue
			}
			sum.Runs++
			switch e.Mode {
			case "filtered":
				sum.Filtered++
			case "fallback":
				sum.Fallback++
			case "raw":
				sum.RawRuns++
			default:
				sum.Passthrough++
			}
			if e.ExitCode != 0 {
				sum.Failed++
			}
			if e.RecoveryAvailable {
				sum.RecoveryAvailable++
				if recordFresh(root, e.ID, until) {
					sum.RecoveryAvailableNow++
				}
			}
			sum.RawStdoutBytes += e.RawStdoutBytes
			sum.RawStderrBytes += e.RawStderrBytes
			sum.EmittedStdoutBytes += e.EmittedStdoutBytes
			sum.EmittedStderrBytes += e.EmittedStderrBytes
			rawTotal += e.RawStdoutBytes + e.RawStderrBytes
			emittedTotal += e.EmittedStdoutBytes + e.EmittedStderrBytes
		}
	}
	sum.EstimatedInputTokens = Tokens(rawTotal)
	sum.EstimatedOutputTokens = Tokens(emittedTotal)
	sum.GrossSavedBytes = rawTotal - emittedTotal
	sum.NetSavedBytes = sum.GrossSavedBytes - sum.RecallBytes
	return sum, nil
}

// recordFresh checks the current on-disk manifest instead of trusting the
// historical recovery_available flag.
func recordFresh(root, id string, now time.Time) bool {
	if !validID(id) {
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, "runs", id, "manifest.json"))
	if err != nil {
		return false
	}
	var m struct {
		Complete  bool      `json:"complete"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if json.Unmarshal(data, &m) != nil {
		return false
	}
	return m.Complete && now.Before(m.ExpiresAt)
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, r := range id {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// ParseSince parses Nd, Nh, Nm, or Ns.
func ParseSince(s string) (time.Duration, error) {
	if s == "" {
		return 7 * 24 * time.Hour, nil
	}
	unit := s[len(s)-1]
	n := s[:len(s)-1]
	if n == "" {
		return 0, errors.New("invalid --since value")
	}
	var mult time.Duration
	switch unit {
	case 'd':
		mult = 24 * time.Hour
	case 'h':
		mult = time.Hour
	case 'm':
		mult = time.Minute
	case 's':
		mult = time.Second
	default:
		return 0, errors.New("--since must look like 7d, 24h, 30m, or 60s")
	}
	var v int64
	for _, r := range n {
		if r < '0' || r > '9' {
			return 0, errors.New("invalid --since value")
		}
		v = v*10 + int64(r-'0')
	}
	if v <= 0 {
		return 0, errors.New("--since must be positive")
	}
	return time.Duration(v) * mult, nil
}

// Clean removes stats events older than retention. It returns removed file
// count and bytes.
func Clean(root string, retentionDays int, now time.Time, dryRun bool) (int, int64, error) {
	base := filepath.Join(root, "stats")
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	cutoff := now.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	var count int
	var bytes int64
	for _, day := range entries {
		if !day.IsDir() {
			continue
		}
		dir := filepath.Join(base, day.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		remaining := 0
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			path := filepath.Join(dir, f.Name())
			info, err := f.Info()
			if err != nil {
				continue
			}
			if !info.ModTime().Before(cutoff) {
				remaining++
				continue
			}
			count++
			bytes += info.Size()
			if !dryRun {
				_ = os.Remove(path)
			}
		}
		if remaining == 0 && !dryRun {
			// Remove only the (now empty) managed date directory.
			_ = os.Remove(dir)
		}
	}
	return count, bytes, nil
}
