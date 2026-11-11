// Package cli implements the rtk-go command surface.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ShawnMa123/token-trim/internal/buildinfo"
	"github.com/ShawnMa123/token-trim/internal/config"
	xexec "github.com/ShawnMa123/token-trim/internal/exec"
	"github.com/ShawnMa123/token-trim/internal/filter"
	"github.com/ShawnMa123/token-trim/internal/recovery"
	"github.com/ShawnMa123/token-trim/internal/stats"
)

// App carries the process streams so tests can drive the CLI in-process.
type App struct {
	Stdin  *os.File
	Stdout io.Writer
	Stderr io.Writer
}

// Run dispatches one rtk-go invocation and returns the process exit code.
func Run(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	a := &App{Stdin: stdin, Stdout: stdout, Stderr: stderr}
	if len(args) == 0 {
		a.usage()
		return 2
	}
	switch args[0] {
	case "exec":
		return a.execCommand(args[1:], false)
	case "bypass":
		return a.execCommand(args[1:], true)
	case "recall":
		return a.recall(args[1:])
	case "rewrite":
		return a.rewrite(args[1:])
	case "hook":
		return a.hook(args[1:])
	case "init":
		return a.initCodex(args[1:])
	case "uninstall":
		return a.uninstallCodex(args[1:])
	case "doctor":
		return a.doctor(args[1:])
	case "gain":
		return a.gain(args[1:])
	case "config":
		return a.configCommand(args[1:])
	case "clean":
		return a.clean(args[1:])
	case "version":
		return a.version(args[1:])
	case "__probe":
		wd, _ := os.Getwd()
		_ = json.NewEncoder(a.Stdout).Encode(map[string]any{
			"argv": os.Args[1:],
			"cwd":  wd,
			"env":  os.Getenv("RTK_GO_PROBE"),
		})
		return 0
	case "__probe-exit":
		if len(args) > 1 {
			if code, err := strconv.Atoi(args[1]); err == nil {
				return code
			}
		}
		return 125
	case "help", "-h", "--help":
		a.usage()
		return 0
	default:
		return a.execCommand(args, false)
	}
}

func (a *App) usage() {
	fmt.Fprint(a.Stderr, `rtk-go - command output trimmer for Codex CLI

Usage:
  rtk-go <program> [args...]
  rtk-go exec [--raw] [--no-store] [--profile safe|balanced|aggressive|lossless] [--encoding auto|utf-8] -- <program> [args...]
  rtk-go bypass <program> [args...]
  rtk-go recall <id> [--stream stdout|stderr|both] [--offset N] [--limit N] [--raw]
  rtk-go rewrite --shell powershell|posix --command <text> --format json
  rtk-go init codex --mode hooks|instructions --scope project|user [--dry-run]
  rtk-go uninstall codex --scope project|user [--dry-run]
  rtk-go doctor [--format json] [--probe]
  rtk-go gain [--since 7d] [--format text|json]
  rtk-go config show [--format json]
  rtk-go clean --expired [--dry-run]
  rtk-go version [--format json]

Exit codes: 0 success; 1 management/internal failure; 2 usage; 125 wrapper
lifecycle failure; 126 not executable; 127 not found.
`)
}

type execOptions struct {
	raw      bool
	noStore  bool
	profile  string
	encoding string
}

func parseExecArgs(tokens []string) (execOptions, []string, error) {
	var opts execOptions
	opts.encoding = "auto"
	i := 0
	for i < len(tokens) {
		t := tokens[i]
		switch {
		case t == "--":
			i++
			if i >= len(tokens) {
				return opts, nil, fmt.Errorf("exec: missing program after --")
			}
			return opts, tokens[i:], nil
		case t == "--raw":
			opts.raw = true
			i++
		case t == "--no-store":
			opts.noStore = true
			i++
		case t == "--profile" || strings.HasPrefix(t, "--profile="):
			v := strings.TrimPrefix(t, "--profile=")
			if v == t {
				if i+1 >= len(tokens) {
					return opts, nil, fmt.Errorf("exec: --profile requires a value")
				}
				i++
				v = tokens[i]
			}
			if v != "safe" && v != "balanced" && v != "aggressive" && v != "lossless" {
				return opts, nil, fmt.Errorf("exec: --profile must be safe, balanced, aggressive, or lossless")
			}
			opts.profile = v
			i++
		case t == "--encoding" || strings.HasPrefix(t, "--encoding="):
			v := strings.TrimPrefix(t, "--encoding=")
			if v == t {
				if i+1 >= len(tokens) {
					return opts, nil, fmt.Errorf("exec: --encoding requires a value")
				}
				i++
				v = tokens[i]
			}
			if v != "auto" && v != "utf-8" {
				return opts, nil, fmt.Errorf("exec: unsupported --encoding %q; v1 supports auto or utf-8 only", v)
			}
			opts.encoding = v
			i++
		case strings.HasPrefix(t, "-"):
			return opts, nil, fmt.Errorf("exec: unknown wrapper option %q (use -- before child flags)", t)
		default:
			return opts, tokens[i:], nil
		}
	}
	return opts, nil, fmt.Errorf("exec: missing program")
}

func (a *App) execCommand(tokens []string, forceRaw bool) int {
	started := time.Now().UTC()
	opts, childArgv, err := parseExecArgs(tokens)
	if err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go: %v\n", err)
		return 2
	}
	if len(childArgv) == 0 {
		fmt.Fprintln(a.Stderr, "rtk-go: missing program")
		return 2
	}

	cfgLoaded := config.Load()
	cfg := cfgLoaded.Config
	effectiveRaw := forceRaw || opts.raw || config.Disabled() || cfgLoaded.Err != nil ||
		os.Getenv("RTK_GO_ACTIVE") == "1" || isTerminalWriter(a.Stdout)
	noStore := opts.noStore || config.NoStore() || !cfg.Storage.Enabled
	profile := opts.profile
	if profile == "" {
		profile = cfg.Profile
	}

	var sel *filter.Selection
	if !effectiveRaw {
		sel = filter.Select(childArgv, profile)
	}

	id, _ := recovery.NewID()
	store := recovery.New(cfg.Storage.DataDir, cfg.Storage.TotalMiB, cfg.Storage.RetentionHours)

	var resv *recovery.Reservation
	captureAllowed := false
	captureReason := ""
	if sel != nil {
		if sel.Lossless {
			captureAllowed = true
		} else if !noStore {
			if r, ok := store.Acquire(id); ok {
				resv = r
				captureAllowed = true
			} else {
				captureReason = "storage unavailable"
			}
		} else {
			captureReason = "storage disabled"
		}
	}
	if resv != nil {
		defer resv.Release()
	}

	var capture *xexec.CapturePolicy
	if captureAllowed {
		window := cfg.Capture.StreamThresholdMS
		if sel.Test {
			window = cfg.Capture.TestThresholdMS
		}
		capture = &xexec.CapturePolicy{
			MemoryLimit: cfg.Capture.MemoryMiB << 20,
			LineLimit:   1 << 20,
			Deadline:    time.Now().Add(time.Duration(window) * time.Millisecond),
		}
	}

	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "RTK_GO_ACTIVE=") {
			continue
		}
		env = append(env, e)
	}
	env = append(env, "RTK_GO_ACTIVE=1")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	res := xexec.Run(ctx, xexec.Spec{
		Program:     childArgv[0],
		Args:        childArgv[1:],
		Env:         env,
		Stdin:       a.Stdin,
		Stdout:      a.Stdout,
		Stderr:      a.Stderr,
		Capture:     capture,
		MirrorLimit: mirrorLimit(resv),
	})

	if !res.Started {
		fmt.Fprintf(a.Stderr, "rtk-go: %s\n", res.SpawnError)
		a.recordStats(cfg, cfgLoaded, id, started, sel, "raw", "", res.SpawnError, false, res, 0)
		return res.ExitCode
	}

	mode := "passthrough"
	parserID := ""
	fallbackReason := ""
	recoveryAvailable := false
	emittedStdout := res.StdoutBytes
	var writeErr error

	if res.StdoutCaptured {
		var finalOut []byte
		if sel != nil && !res.IncompleteOutput {
			out, why := filter.Run(sel.Filter, filter.Input{Argv: childArgv, ExitCode: res.ExitCode, Stdout: res.Stdout, Profile: profile})
			switch {
			case why != "":
				fallbackReason = why
			case out.Lossy:
				if resv == nil || !res.StderrComplete {
					fallbackReason = "complete original unavailable"
				} else if _, err := resv.Commit(id, res.Stdout, res.Stderr, res.ExitCode, "utf-8"); err != nil {
					fallbackReason = "recovery commit failed"
				} else {
					hint := fmt.Sprintf("[rtk-go recall %s]\n", id)
					if cand := filter.BuildCandidate(res.Stdout, out, hint); cand != nil {
						finalOut = cand
						mode = "filtered"
						parserID = out.ParserID
						recoveryAvailable = true
					} else {
						fallbackReason = "byte guard"
					}
				}
			default:
				if cand := filter.BuildCandidate(res.Stdout, out, ""); cand != nil {
					finalOut = cand
					mode = "filtered"
					parserID = out.ParserID
				} else {
					fallbackReason = "byte guard"
				}
			}
			if finalOut == nil {
				mode = "fallback"
			}
		} else if res.IncompleteOutput {
			fallbackReason = "incomplete output"
			mode = "fallback"
		}
		if finalOut == nil {
			finalOut = res.Stdout
		}
		n, err := a.Stdout.Write(finalOut)
		emittedStdout = int64(n)
		if err == nil && n != len(finalOut) {
			err = io.ErrShortWrite
		}
		writeErr = err
	} else if sel != nil && capture == nil && captureReason != "" {
		mode = "fallback"
		fallbackReason = captureReason
	} else if effectiveRaw {
		mode = "raw"
	}

	if res.IncompleteOutput {
		fmt.Fprintln(a.Stderr, "rtk-go: incomplete output: a background descendant kept the output pipe open and was terminated")
	}

	if writeErr != nil {
		fmt.Fprintf(a.Stderr, "rtk-go: writing stdout: %v\n", writeErr)
		if res.ExitCode == 0 {
			res.ExitCode = 125
		}
	}
	if res.OutputError != nil {
		fmt.Fprintf(a.Stderr, "rtk-go: output write failed: %v\n", res.OutputError)
		if res.ExitCode == 0 {
			res.ExitCode = 125
		}
	}

	a.recordStats(cfg, cfgLoaded, id, started, sel, mode, parserID, fallbackReason, recoveryAvailable, res, emittedStdout)
	return res.ExitCode
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func mirrorLimit(resv *recovery.Reservation) int {
	if resv == nil {
		return 0
	}
	return 31 << 20
}

func commandFamily(sel *filter.Selection, resolved string) string {
	if sel != nil {
		return sel.Family
	}
	if resolved == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(filepath.Base(resolved), ".exe"))
}

func (a *App) recordStats(cfg config.Config, loaded config.Loaded, id string, started time.Time, sel *filter.Selection, mode, parserID, fallbackReason string, recoveryAvailable bool, res xexec.Result, emittedStdout int64) {
	if !cfg.Stats.Enabled || loaded.Err != nil {
		return
	}
	if id == "" {
		id, _ = recovery.NewID()
	}
	store := recovery.New(cfg.Storage.DataDir, cfg.Storage.TotalMiB, cfg.Storage.RetentionHours)
	if _, err := store.Check(); err != nil {
		return
	}
	if err := store.Prepare(); err != nil {
		return
	}
	family := commandFamily(sel, res.Resolved)
	_ = stats.Write(cfg.Storage.DataDir, stats.Event{
		ID:                 id,
		StartedAtUTC:       started,
		CommandFamily:      family,
		ParserID:           parserID,
		Mode:               mode,
		ExitCode:           res.ExitCode,
		DurationMS:         res.Duration.Milliseconds(),
		RawStdoutBytes:     res.StdoutBytes,
		RawStderrBytes:     res.StderrBytes,
		EmittedStdoutBytes: emittedStdout,
		EmittedStderrBytes: res.StderrBytes,
		FallbackReason:     fallbackReason,
		RecoveryAvailable:  recoveryAvailable,
	})
}

// versionJSON is the stable version payload.
type versionJSON struct {
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	Go            string `json:"go"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	SchemaVersion int    `json:"schema_version"`
}

func (a *App) version(args []string) int {
	format := "text"
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--format" && i+1 < len(args):
			i++
			format = args[i]
		case strings.HasPrefix(args[i], "--format="):
			format = strings.TrimPrefix(args[i], "--format=")
		default:
			fmt.Fprintf(a.Stderr, "rtk-go version: unexpected argument %q\n", args[i])
			return 2
		}
	}
	v := versionJSON{
		Version: buildinfo.Version, Commit: buildinfo.Commit,
		Go: buildinfo.GoVersion(), OS: buildinfo.OS(), Arch: buildinfo.Arch(),
		SchemaVersion: config.SchemaVersion,
	}
	if format == "json" {
		_ = json.NewEncoder(a.Stdout).Encode(v)
		return 0
	}
	if format != "text" {
		fmt.Fprintln(a.Stderr, "rtk-go version: --format must be text or json")
		return 2
	}
	fmt.Fprintf(a.Stdout, "rtk-go %s (commit %s)\n", v.Version, v.Commit)
	fmt.Fprintf(a.Stdout, "go: %s %s/%s\n", v.Go, v.OS, v.Arch)
	fmt.Fprintf(a.Stdout, "storage schema: %d\n", v.SchemaVersion)
	return 0
}
