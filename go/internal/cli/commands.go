package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ShawnMa123/token-trim/go/internal/buildinfo"
	"github.com/ShawnMa123/token-trim/go/internal/config"
	"github.com/ShawnMa123/token-trim/go/internal/recovery"
	"github.com/ShawnMa123/token-trim/go/internal/stats"
)

func (a *App) recall(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(a.Stderr, "rtk-go recall: missing id")
		return 2
	}
	var (
		id     string
		stream = "both"
		offset = 1
		limit  = 200
		raw    bool
	)
	for i := 0; i < len(args); i++ {
		t := args[i]
		need := func(name string) (string, bool) {
			if i+1 >= len(args) {
				fmt.Fprintf(a.Stderr, "rtk-go recall: %s requires a value\n", name)
				return "", false
			}
			i++
			return args[i], true
		}
		switch {
		case t == "--stream":
			v, ok := need(t)
			if !ok {
				return 2
			}
			stream = v
		case strings.HasPrefix(t, "--stream="):
			stream = strings.TrimPrefix(t, "--stream=")
		case t == "--offset":
			v, ok := need(t)
			if !ok {
				return 2
			}
			offset, _ = strconv.Atoi(v)
		case strings.HasPrefix(t, "--offset="):
			offset, _ = strconv.Atoi(strings.TrimPrefix(t, "--offset="))
		case t == "--limit":
			v, ok := need(t)
			if !ok {
				return 2
			}
			limit, _ = strconv.Atoi(v)
		case strings.HasPrefix(t, "--limit="):
			limit, _ = strconv.Atoi(strings.TrimPrefix(t, "--limit="))
		case t == "--raw":
			raw = true
		case strings.HasPrefix(t, "-"):
			fmt.Fprintf(a.Stderr, "rtk-go recall: unexpected option %q\n", t)
			return 2
		default:
			if id != "" {
				fmt.Fprintln(a.Stderr, "rtk-go recall: exactly one id is required")
				return 2
			}
			id = t
		}
	}
	if id == "" {
		fmt.Fprintln(a.Stderr, "rtk-go recall: missing id")
		return 2
	}
	if !recovery.ValidID(id) {
		fmt.Fprintln(a.Stderr, "rtk-go recall: invalid id (expected 32 lowercase hex characters)")
		return 2
	}
	if stream != "stdout" && stream != "stderr" && stream != "both" {
		fmt.Fprintln(a.Stderr, "rtk-go recall: --stream must be stdout, stderr, or both")
		return 2
	}
	if offset < 1 || limit < 1 || limit > 5000 {
		fmt.Fprintln(a.Stderr, "rtk-go recall: offset must be >= 1 and limit 1..5000")
		return 2
	}
	if raw && stream == "both" {
		fmt.Fprintln(a.Stderr, "rtk-go recall: --raw requires a single --stream")
		return 2
	}

	loaded := config.Load()
	if loaded.Err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go recall: invalid config: %v\n", loaded.Err)
		return 1
	}
	cfg := loaded.Config
	store := recovery.New(cfg.Storage.DataDir, cfg.Storage.TotalMiB, cfg.Storage.RetentionHours)
	h, err := store.Open(id)
	if err != nil {
		switch {
		case errors.Is(err, recovery.ErrNotFound):
			fmt.Fprintf(a.Stderr, "rtk-go recall: record not found: %s\n", id)
		case errors.Is(err, recovery.ErrExpired):
			fmt.Fprintf(a.Stderr, "rtk-go recall: record expired: %s\n", id)
		case errors.Is(err, recovery.ErrCorrupt):
			fmt.Fprintf(a.Stderr, "rtk-go recall: record corrupt: %v\n", err)
		default:
			fmt.Fprintf(a.Stderr, "rtk-go recall: %v\n", err)
		}
		return 1
	}

	var written int64
	if raw {
		written, err = h.Raw(stream, a.Stdout)
	} else if stream == "both" {
		var n int64
		n, err = h.Text("stdout", a.Stdout, offset, limit)
		written += n
		if err == nil && h.StderrBytes > 0 {
			n, err = h.Text("stderr", a.Stdout, offset, limit)
			written += n
		}
	} else {
		written, err = h.Text(stream, a.Stdout, offset, limit)
	}
	if err != nil {
		if errors.Is(err, recovery.ErrNotText) {
			fmt.Fprintf(a.Stderr, "rtk-go recall: stream is not valid UTF-8; use --stream stdout|stderr --raw\n")
		} else {
			fmt.Fprintf(a.Stderr, "rtk-go recall: %v\n", err)
		}
		return 1
	}
	a.recordRecall(cfg, written)
	return 0
}

func (a *App) recordRecall(cfg config.Config, bytes int64) {
	if !cfg.Stats.Enabled {
		return
	}
	store := recovery.New(cfg.Storage.DataDir, cfg.Storage.TotalMiB, cfg.Storage.RetentionHours)
	if _, err := store.Check(); err != nil {
		return
	}
	if err := store.Prepare(); err != nil {
		return
	}
	id, err := recovery.NewID()
	if err != nil {
		return
	}
	_ = stats.Write(cfg.Storage.DataDir, stats.Event{
		ID:                 id,
		StartedAtUTC:       time.Now().UTC(),
		CommandFamily:      "recall",
		Mode:               "recall",
		EmittedStdoutBytes: bytes,
	})
}

func (a *App) gain(args []string) int {
	since := "7d"
	format := "text"
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--since" && i+1 < len(args):
			i++
			since = args[i]
		case strings.HasPrefix(args[i], "--since="):
			since = strings.TrimPrefix(args[i], "--since=")
		case args[i] == "--format" && i+1 < len(args):
			i++
			format = args[i]
		case strings.HasPrefix(args[i], "--format="):
			format = strings.TrimPrefix(args[i], "--format=")
		default:
			fmt.Fprintf(a.Stderr, "rtk-go gain: unexpected argument %q\n", args[i])
			return 2
		}
	}
	d, err := stats.ParseSince(since)
	if err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go gain: %v\n", err)
		return 2
	}
	loaded := config.Load()
	if loaded.Err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go gain: invalid config: %v\n", loaded.Err)
		return 1
	}
	cfg := loaded.Config
	now := time.Now().UTC()
	sum, err := stats.Gain(cfg.Storage.DataDir, now.Add(-d), now)
	if err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go gain: %v\n", err)
		return 1
	}
	if format == "json" {
		_ = json.NewEncoder(a.Stdout).Encode(sum)
		return 0
	}
	if format != "text" {
		fmt.Fprintln(a.Stderr, "rtk-go gain: --format must be text or json")
		return 2
	}
	if sum.Runs == 0 && sum.RecallCount == 0 {
		fmt.Fprintf(a.Stdout, "no data in the last %s\n", since)
		return 0
	}
	fmt.Fprintf(a.Stdout, "runs: %d (filtered %d, fallback %d, passthrough %d, raw %d)\n",
		sum.Runs, sum.Filtered, sum.Fallback, sum.Passthrough, sum.RawRuns)
	fmt.Fprintf(a.Stdout, "failed commands: %d\n", sum.Failed)
	fmt.Fprintf(a.Stdout, "raw bytes: %d (stdout %d, stderr %d)\n", sum.RawStdoutBytes+sum.RawStderrBytes, sum.RawStdoutBytes, sum.RawStderrBytes)
	fmt.Fprintf(a.Stdout, "emitted bytes: %d (stdout %d, stderr %d)\n", sum.EmittedStdoutBytes+sum.EmittedStderrBytes, sum.EmittedStdoutBytes, sum.EmittedStderrBytes)
	fmt.Fprintf(a.Stdout, "estimated tokens: %d -> %d (%s)\n", sum.EstimatedInputTokens, sum.EstimatedOutputTokens, sum.EstimateMethod)
	fmt.Fprintf(a.Stdout, "gross saved: %d bytes\n", sum.GrossSavedBytes)
	if sum.RecoveryAvailable > 0 {
		fmt.Fprintf(a.Stdout, "recovery records: %d of %d still readable\n", sum.RecoveryAvailableNow, sum.RecoveryAvailable)
	}
	fmt.Fprintf(a.Stdout, "recall: %d reads, %d bytes\n", sum.RecallCount, sum.RecallBytes)
	fmt.Fprintf(a.Stdout, "net saved: %d bytes\n", sum.NetSavedBytes)
	if sum.CorruptEvents > 0 {
		fmt.Fprintf(a.Stdout, "corrupt events ignored: %d\n", sum.CorruptEvents)
	}
	return 0
}

func (a *App) configCommand(args []string) int {
	if len(args) == 0 || args[0] != "show" {
		fmt.Fprintln(a.Stderr, "rtk-go config: expected 'show'")
		return 2
	}
	format := "text"
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "--format" && i+1 < len(args):
			i++
			format = args[i]
		case strings.HasPrefix(args[i], "--format="):
			format = strings.TrimPrefix(args[i], "--format=")
		default:
			fmt.Fprintf(a.Stderr, "rtk-go config show: unexpected argument %q\n", args[i])
			return 2
		}
	}
	loaded := config.Load()
	if format == "json" {
		payload := map[string]any{
			"path":     loaded.Path,
			"source":   loaded.Source,
			"config":   loaded.Config,
			"warnings": loaded.Warnings,
		}
		if loaded.Err != nil {
			payload["error"] = loaded.Err.Error()
		}
		_ = json.NewEncoder(a.Stdout).Encode(payload)
		if loaded.Err != nil {
			return 1
		}
		return 0
	}
	if format != "text" {
		fmt.Fprintln(a.Stderr, "rtk-go config show: --format must be text or json")
		return 2
	}
	c := loaded.Config
	fmt.Fprintf(a.Stdout, "path: %s\nsource: %s\n", loaded.Path, loaded.Source)
	fmt.Fprintf(a.Stdout, "schema_version: %d\nprofile: %s\n", c.SchemaVersion, c.Profile)
	fmt.Fprintf(a.Stdout, "storage.enabled: %v\nstorage.data_dir: %s\n", c.Storage.Enabled, c.Storage.DataDir)
	fmt.Fprintf(a.Stdout, "storage.retention_hours: %d\nstorage.total_mib: %d\n", c.Storage.RetentionHours, c.Storage.TotalMiB)
	fmt.Fprintf(a.Stdout, "capture.memory_mib: %d\ncapture.stream_threshold_ms: %d\ncapture.test_threshold_ms: %d\n",
		c.Capture.MemoryMiB, c.Capture.StreamThresholdMS, c.Capture.TestThresholdMS)
	fmt.Fprintf(a.Stdout, "stats.enabled: %v\nstats.retention_days: %d\n", c.Stats.Enabled, c.Stats.RetentionDays)
	fmt.Fprintf(a.Stdout, "codex.auto_rewrite: %v\ncodex.rewrite_tests: %v\n", c.Codex.AutoRewrite, c.Codex.RewriteTests)
	for _, w := range loaded.Warnings {
		fmt.Fprintf(a.Stdout, "warning: %s\n", w)
	}
	if loaded.Err != nil {
		fmt.Fprintf(a.Stdout, "error: %v\n", loaded.Err)
		return 1
	}
	return 0
}

func (a *App) clean(args []string) int {
	expired := false
	dryRun := false
	for _, t := range args {
		switch t {
		case "--expired":
			expired = true
		case "--dry-run":
			dryRun = true
		default:
			fmt.Fprintf(a.Stderr, "rtk-go clean: unexpected argument %q\n", t)
			return 2
		}
	}
	if !expired {
		fmt.Fprintln(a.Stderr, "rtk-go clean: --expired is required")
		return 2
	}
	loaded := config.Load()
	if loaded.Err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go clean: invalid config: %v\n", loaded.Err)
		return 1
	}
	cfg := loaded.Config
	store := recovery.New(cfg.Storage.DataDir, cfg.Storage.TotalMiB, cfg.Storage.RetentionHours)
	now := time.Now()
	entries, err := store.CleanExpired(dryRun, now)
	if err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go clean: %v\n", err)
		return 1
	}
	var total int64
	for _, e := range entries {
		total += e.Bytes
		verb := "removed"
		if dryRun {
			verb = "would remove"
		}
		fmt.Fprintf(a.Stdout, "%s %s %s\n", verb, e.ID, config.FileSize(e.Bytes))
	}
	if len(entries) == 0 {
		fmt.Fprintln(a.Stdout, "nothing to clean")
	} else {
		fmt.Fprintf(a.Stdout, "total: %d objects, %s\n", len(entries), config.FileSize(total))
	}
	count, bytes, err := stats.Clean(cfg.Storage.DataDir, cfg.Stats.RetentionDays, now, dryRun)
	if err == nil && count > 0 {
		verb := "removed"
		if dryRun {
			verb = "would remove"
		}
		fmt.Fprintf(a.Stdout, "stats: %s %d events, %s\n", verb, count, config.FileSize(bytes))
	}
	return 0
}

type checkResult struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok, warn, fail, info
	Detail string `json:"detail"`
}

func (a *App) doctor(args []string) int {
	format := "text"
	probe := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--format" && i+1 < len(args):
			i++
			format = args[i]
		case strings.HasPrefix(args[i], "--format="):
			format = strings.TrimPrefix(args[i], "--format=")
		case args[i] == "--probe":
			probe = true
		default:
			fmt.Fprintf(a.Stderr, "rtk-go doctor: unexpected argument %q\n", args[i])
			return 2
		}
	}
	if format != "text" && format != "json" {
		fmt.Fprintln(a.Stderr, "rtk-go doctor: --format must be text or json")
		return 2
	}

	loaded := config.Load()
	cfg := loaded.Config
	var checks []checkResult
	add := func(name, status, detail string) {
		checks = append(checks, checkResult{Name: name, Status: status, Detail: detail})
	}

	add("version", "info", fmt.Sprintf("%s (%s) %s %s/%s", buildinfo.Version, buildinfo.Commit, buildinfo.GoVersion(), buildinfo.OS(), buildinfo.Arch()))
	if loaded.Err != nil {
		add("config", "fail", fmt.Sprintf("%s: %v", loaded.Path, loaded.Err))
	} else {
		status := "ok"
		detail := fmt.Sprintf("%s (%s)", loaded.Path, loaded.Source)
		if len(loaded.Warnings) > 0 {
			status = "warn"
			detail += "; " + strings.Join(loaded.Warnings, "; ")
		}
		add("config", status, detail)
	}

	store := recovery.New(cfg.Storage.DataDir, cfg.Storage.TotalMiB, cfg.Storage.RetentionHours)
	if cfg.Storage.Enabled {
		exists, err := store.Check()
		switch {
		case err != nil:
			add("storage privacy", "fail", fmt.Sprintf("%s: %v", cfg.Storage.DataDir, err))
		case !exists:
			add("storage privacy", "info", cfg.Storage.DataDir+" (not created yet)")
		default:
			add("storage privacy", "ok", cfg.Storage.DataDir+" (owner-only)")
		}
	} else {
		add("storage privacy", "info", "storage disabled; no original output is written")
	}
	add("storage", "info", fmt.Sprintf("enabled=%v retention=%dh quota=%dMiB", cfg.Storage.Enabled, cfg.Storage.RetentionHours, cfg.Storage.TotalMiB))
	add("profile", "info", cfg.Profile)

	if shell := os.Getenv("COMSPEC"); shell != "" {
		add("shell", "info", shell)
	} else if shell := os.Getenv("SHELL"); shell != "" {
		add("shell", "info", shell)
	} else {
		add("shell", "info", "unknown")
	}

	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			codexHome = filepath.Join(home, ".codex")
		}
	}
	add("codex home", "info", codexHome)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	out, verr := osexec.CommandContext(ctx, "codex", "--version").Output()
	cancel()
	if verr != nil {
		add("codex cli", "warn", "not found; manual wrapping still works")
	} else {
		add("codex cli", "info", strings.TrimSpace(string(out)))
	}

	installed := installedIntegrations(codexHome)
	if len(installed) == 0 {
		add("codex integration", "info", "manual-only (no managed instructions or hooks)")
	} else {
		add("codex integration", "ok", strings.Join(installed, ", ")+"; hooks remain gated")
	}

	probeFailed := false
	if probe {
		if err := a.runProbe(); err != nil {
			probeFailed = true
			add("probe", "fail", err.Error())
		} else {
			add("probe", "ok", "argv/cwd/env/exit fidelity verified locally")
		}
	}

	failed := loaded.Err != nil || probeFailed
	for _, c := range checks {
		if c.Status == "fail" {
			failed = true
		}
	}

	if format == "json" {
		_ = json.NewEncoder(a.Stdout).Encode(map[string]any{
			"checks": checks,
			"ok":     !failed,
		})
	} else {
		for _, c := range checks {
			fmt.Fprintf(a.Stdout, "%-16s %-4s %s\n", c.Name+":", c.Status, c.Detail)
		}
	}
	if failed {
		return 1
	}
	return 0
}

func installedIntegrations(codexHome string) []string {
	var found []string
	cwd, _ := os.Getwd()
	candidates := []struct{ scope, path string }{
		{"project", filepath.Join(cwd, "AGENTS.md")},
		{"user", filepath.Join(codexHome, "AGENTS.md")},
	}
	for _, c := range candidates {
		data, err := os.ReadFile(c.path)
		if err == nil && strings.Contains(string(data), "<!-- rtk-go:begin -->") {
			found = append(found, c.scope+" instructions")
		}
	}
	for _, scope := range []string{"project", "user"} {
		p := filepath.Join(cwd, ".codex", "hooks.json")
		if scope == "user" {
			p = filepath.Join(codexHome, "hooks.json")
		}
		data, err := os.ReadFile(p)
		if err == nil && strings.Contains(string(data), "rtk-go hook codex") {
			found = append(found, scope+" hooks")
		}
	}
	return found
}

func (a *App) runProbe() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := a.probeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := osexec.CommandContext(ctx, exe, "__probe", "arg with space", "中文")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "RTK_GO_PROBE=probe-value")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("probe execution failed: %v", err)
	}
	var got struct {
		Argv []string `json:"argv"`
		Cwd  string   `json:"cwd"`
		Env  string   `json:"env"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		return fmt.Errorf("probe output invalid: %v", err)
	}
	if len(got.Argv) != 3 || got.Argv[1] != "arg with space" || got.Argv[2] != "中文" {
		return fmt.Errorf("probe argv mismatch: %q", got.Argv)
	}
	realDir, _ := filepath.EvalSymlinks(dir)
	realGot, _ := filepath.EvalSymlinks(got.Cwd)
	if !strings.EqualFold(realDir, realGot) {
		return fmt.Errorf("probe cwd mismatch")
	}
	if got.Env != "probe-value" {
		return fmt.Errorf("probe env mismatch")
	}
	exitCmd := osexec.CommandContext(ctx, exe, "__probe-exit", "7")
	if err := exitCmd.Run(); err == nil {
		return fmt.Errorf("probe exit status was not preserved")
	} else if ee, ok := err.(*osexec.ExitError); !ok || ee.ExitCode() != 7 {
		return fmt.Errorf("probe exit status: %v", err)
	}
	return nil
}

func (a *App) probeDir() string {
	return filepath.Join(os.TempDir(), "rtk-go-probe-"+strconv.FormatInt(time.Now().UnixNano(), 36))
}
