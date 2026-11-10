// Package config implements the schema=1 JSON configuration contract.
//
// Precedence is: built-in defaults < user config file (or RTK_GO_CONFIG) <
// environment switches < wrapper flags. RTK_GO_DISABLE and RTK_GO_NO_STORE are
// emergency switches that wrapper flags cannot turn back on.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// SchemaVersion is the only supported configuration schema.
const SchemaVersion = 1

// Storage controls on-disk recovery storage.
type Storage struct {
	Enabled        bool   `json:"enabled"`
	DataDir        string `json:"data_dir"`
	RetentionHours int    `json:"retention_hours"`
	TotalMiB       int    `json:"total_mib"`
}

// Capture controls bounded stdout capture.
type Capture struct {
	MemoryMiB         int `json:"memory_mib"`
	StreamThresholdMS int `json:"stream_threshold_ms"`
	TestThresholdMS   int `json:"test_threshold_ms"`
}

// Stats controls local invocation metadata.
type Stats struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retention_days"`
}

// Codex controls the Codex CLI integration layer.
type Codex struct {
	AutoRewrite  bool `json:"auto_rewrite"`
	RewriteTests bool `json:"rewrite_tests"`
}

// Config is the effective configuration.
type Config struct {
	SchemaVersion int     `json:"schema_version"`
	Profile       string  `json:"profile"`
	Storage       Storage `json:"storage"`
	Capture       Capture `json:"capture"`
	Stats         Stats   `json:"stats"`
	Codex         Codex   `json:"codex"`

	// DataDirExplicit records whether storage.data_dir came from the file.
	DataDirExplicit bool `json:"-"`
}

// Loaded is the result of loading configuration.
type Loaded struct {
	Config   Config
	Path     string
	Source   string // "default", "file", "env"
	Warnings []string
	// Err is non-nil when the file is unreadable, malformed, or violates the
	// schema. Callers must run the child unchanged (raw) in that case.
	Err error
}

// Default returns the built-in defaults.
func Default() Config {
	c := Config{
		SchemaVersion: SchemaVersion,
		Profile:       "aggressive",
		Storage: Storage{
			Enabled:        true,
			RetentionHours: 24,
			TotalMiB:       256,
		},
		Capture: Capture{
			MemoryMiB:         8,
			StreamThresholdMS: 1500,
			TestThresholdMS:   30000,
		},
		Stats: Stats{Enabled: true, RetentionDays: 30},
		Codex: Codex{AutoRewrite: false, RewriteTests: false},
	}
	c.Storage.DataDir = defaultDataDir()
	return c
}

func defaultDataDir() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		if home, herr := os.UserHomeDir(); herr == nil && home != "" {
			return filepath.Join(home, ".cache", "rtk-go")
		}
		return filepath.Join(os.TempDir(), "rtk-go")
	}
	return filepath.Join(dir, "rtk-go")
}

// DefaultConfigPath resolves the user configuration path.
func DefaultConfigPath() string {
	if p := os.Getenv("RTK_GO_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "rtk-go", "config.json")
}

// Load reads configuration. RTK_GO_CONFIG explicitly selects a file; no
// repository-local configuration is ever discovered or executed.
func Load() Loaded {
	c := Default()
	path := DefaultConfigPath()
	out := Loaded{Config: c, Path: path, Source: "default"}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out
		}
		out.Err = fmt.Errorf("read config %s: %w", path, err)
		return out
	}
	out.Source = "file"

	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var rawTop map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawTop); err != nil {
		out.Err = fmt.Errorf("parse config %s: %w", path, err)
		return out
	}
	out.Warnings = append(out.Warnings, unknownKeys(rawTop)...)

	if err := json.Unmarshal(data, &c); err != nil {
		out.Err = fmt.Errorf("parse config %s: %w", path, err)
		return out
	}
	if _, ok := rawTop["storage"]; ok {
		var st map[string]json.RawMessage
		if json.Unmarshal(rawTop["storage"], &st) == nil {
			if _, ok := st["data_dir"]; ok {
				c.DataDirExplicit = true
			}
		}
	}
	if c.Storage.DataDir == "" {
		c.Storage.DataDir = defaultDataDir()
	}
	out.Config = c
	if err := c.Validate(); err != nil {
		out.Err = fmt.Errorf("invalid config %s: %w", path, err)
	}
	return out
}

var topKeys = map[string][]string{
	"schema_version": nil,
	"profile":        nil,
	"storage":        {"enabled", "data_dir", "retention_hours", "total_mib"},
	"capture":        {"memory_mib", "stream_threshold_ms", "test_threshold_ms"},
	"stats":          {"enabled", "retention_days"},
	"codex":          {"auto_rewrite", "rewrite_tests"},
}

func unknownKeys(top map[string]json.RawMessage) []string {
	var warns []string
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		allowed, ok := topKeys[k]
		if !ok {
			warns = append(warns, "unknown config key: "+k)
			continue
		}
		if allowed == nil {
			continue
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(top[k], &nested) != nil {
			continue
		}
		names := make([]string, 0, len(nested))
		for n := range nested {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			found := false
			for _, a := range allowed {
				if a == n {
					found = true
					break
				}
			}
			if !found {
				warns = append(warns, "unknown config key: "+k+"."+n)
			}
		}
	}
	return warns
}

// Validate checks schema and value ranges.
func (c Config) Validate() error {
	var errs []string
	add := func(path, msg string) { errs = append(errs, path+": "+msg) }

	if c.SchemaVersion != SchemaVersion {
		add("schema_version", fmt.Sprintf("unsupported version %d", c.SchemaVersion))
	}
	if c.Profile != "safe" && c.Profile != "balanced" && c.Profile != "aggressive" {
		add("profile", "must be safe, balanced, or aggressive")
	}
	if !(c.Storage.RetentionHours >= 1 && c.Storage.RetentionHours <= 168) {
		add("storage.retention_hours", "must be 1..168")
	}
	if !(c.Storage.TotalMiB >= 64 && c.Storage.TotalMiB <= 4096) {
		add("storage.total_mib", "must be 64..4096")
	}
	if c.Storage.DataDir == "" {
		add("storage.data_dir", "must not be empty")
	} else if c.DataDirExplicit && !filepath.IsAbs(c.Storage.DataDir) {
		add("storage.data_dir", "must be an absolute path")
	}
	if !(c.Capture.MemoryMiB >= 1 && c.Capture.MemoryMiB <= 32) {
		add("capture.memory_mib", "must be 1..32")
	}
	if !(c.Capture.StreamThresholdMS >= 100 && c.Capture.StreamThresholdMS <= 5000) {
		add("capture.stream_threshold_ms", "must be 100..5000")
	}
	if !(c.Capture.TestThresholdMS >= 1500 && c.Capture.TestThresholdMS <= 120000) {
		add("capture.test_threshold_ms", "must be 1500..120000")
	}
	if !(c.Stats.RetentionDays >= 1 && c.Stats.RetentionDays <= 90) {
		add("stats.retention_days", "must be 1..90")
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, "; "))
}

// EnvBool reports whether the environment variable is set to a true value.
func EnvBool(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Disabled reports the emergency disable switch.
func Disabled() bool { return EnvBool("RTK_GO_DISABLE") }

// NoStore reports the emergency no-store switch.
func NoStore() bool { return EnvBool("RTK_GO_NO_STORE") }

// FileSize returns a human byte size, used by clean/dry-run output.
func FileSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
