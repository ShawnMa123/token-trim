package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ShawnMa123/token-trim/go/internal/codex"
	"github.com/ShawnMa123/token-trim/go/internal/config"
)

const (
	managedBegin = "<!-- rtk-go:begin -->"
	managedEnd   = "<!-- rtk-go:end -->"
)

const instructionsBlock = managedBegin + `
## rtk-go command output

Use ` + "`rtk-go exec -- <command>`" + ` for supported read-only queries (git status, git log, rg).
When output is summarized, recover the full original with ` + "`rtk-go recall <id>`" + `; never rerun a command just to read output.
Do not wrap machine-readable output, pipelines, redirections, tests, builds, or write commands.
` + managedEnd + `
`

func (a *App) rewrite(args []string) int {
	shell, command, format := "", "", "json"
	for i := 0; i < len(args); i++ {
		need := func(name string) (string, bool) {
			if i+1 >= len(args) {
				fmt.Fprintf(a.Stderr, "rtk-go rewrite: %s requires a value\n", name)
				return "", false
			}
			i++
			return args[i], true
		}
		switch {
		case args[i] == "--shell":
			v, ok := need("--shell")
			if !ok {
				return 2
			}
			shell = v
		case strings.HasPrefix(args[i], "--shell="):
			shell = strings.TrimPrefix(args[i], "--shell=")
		case args[i] == "--command":
			v, ok := need("--command")
			if !ok {
				return 2
			}
			command = v
		case strings.HasPrefix(args[i], "--command="):
			command = strings.TrimPrefix(args[i], "--command=")
		case args[i] == "--format":
			v, ok := need("--format")
			if !ok {
				return 2
			}
			format = v
		case strings.HasPrefix(args[i], "--format="):
			format = strings.TrimPrefix(args[i], "--format=")
		default:
			fmt.Fprintf(a.Stderr, "rtk-go rewrite: unexpected argument %q\n", args[i])
			return 2
		}
	}
	if shell != "powershell" && shell != "posix" {
		fmt.Fprintln(a.Stderr, "rtk-go rewrite: --shell must be powershell or posix")
		return 2
	}
	if command == "" {
		fmt.Fprintln(a.Stderr, "rtk-go rewrite: --command is required")
		return 2
	}
	if format != "json" {
		fmt.Fprintln(a.Stderr, "rtk-go rewrite: --format must be json")
		return 2
	}
	binary, err := os.Executable()
	if err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go rewrite: %v\n", err)
		return 1
	}
	if config.Disabled() {
		_ = json.NewEncoder(a.Stdout).Encode(codex.RewriteResult{
			Decision: codex.DecisionPassthrough, Reason: "disabled by RTK_GO_DISABLE=1",
		})
		return 0
	}
	allowTests := false
	if loaded := config.Load(); loaded.Err == nil {
		allowTests = loaded.Config.Codex.RewriteTests
	}
	_ = json.NewEncoder(a.Stdout).Encode(codex.Analyze(shell, command, binary, allowTests))
	return 0
}

func (a *App) hook(args []string) int {
	if len(args) != 1 || args[0] != "codex" {
		fmt.Fprintln(a.Stderr, "rtk-go hook: only 'hook codex' is supported")
		return 2
	}
	// Fail-open no-op. Hook rewriting is not enabled without a verified
	// Codex/platform permission regression; stdout must contain JSON only.
	if a.Stdin != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(a.Stdin, 1<<20))
	}
	fmt.Fprintln(a.Stdout, "{}")
	return 0
}

func (a *App) initCodex(args []string) int {
	mode, scope := "", ""
	dryRun := false
	for i := 0; i < len(args); i++ {
		need := func(name string) (string, bool) {
			if i+1 >= len(args) {
				fmt.Fprintf(a.Stderr, "rtk-go init: %s requires a value\n", name)
				return "", false
			}
			i++
			return args[i], true
		}
		switch {
		case args[i] == "codex":
		case args[i] == "--mode":
			v, ok := need("--mode")
			if !ok {
				return 2
			}
			mode = v
		case strings.HasPrefix(args[i], "--mode="):
			mode = strings.TrimPrefix(args[i], "--mode=")
		case args[i] == "--scope":
			v, ok := need("--scope")
			if !ok {
				return 2
			}
			scope = v
		case strings.HasPrefix(args[i], "--scope="):
			scope = strings.TrimPrefix(args[i], "--scope=")
		case args[i] == "--dry-run":
			dryRun = true
		default:
			fmt.Fprintf(a.Stderr, "rtk-go init: unexpected argument %q\n", args[i])
			return 2
		}
	}
	if mode != "hooks" && mode != "instructions" {
		fmt.Fprintln(a.Stderr, "rtk-go init codex: --mode must be hooks or instructions")
		return 2
	}
	if scope != "project" && scope != "user" {
		fmt.Fprintln(a.Stderr, "rtk-go init codex: --scope must be project or user")
		return 2
	}
	if mode == "hooks" {
		fmt.Fprintln(a.Stderr, "rtk-go init codex: hook mode is not enabled: no verified Codex/platform permission regression; use manual exec or --mode instructions")
		return 1
	}
	target, err := integrationTarget(scope, "AGENTS.md")
	if err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go init codex: %v\n", err)
		return 1
	}
	existing, modeBits, err := readTarget(target)
	if err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go init codex: %v\n", err)
		return 1
	}
	if otherManagedBlock(existing) {
		fmt.Fprintf(a.Stderr, "rtk-go init codex: %s already contains another RTK managed block; refusing to double-install\n", target)
		return 1
	}
	if strings.Contains(existing, managedBegin) && !strings.Contains(existing, managedEnd) {
		fmt.Fprintf(a.Stderr, "rtk-go init codex: %s contains a malformed rtk-go block; fix it first\n", target)
		return 1
	}
	if hasBlock(existing) {
		fmt.Fprintf(a.Stdout, "already installed: %s\n", target)
		return 0
	}
	updated := appendBlock(existing, instructionsBlock)
	if dryRun {
		fmt.Fprintf(a.Stdout, "would update %s\n", target)
		fmt.Fprint(a.Stdout, instructionsBlock)
		return 0
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go init codex: %v\n", err)
		return 1
	}
	if err := writeAtomicWithBackup(target, existing, updated, modeBits); err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go init codex: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Stdout, "installed instructions at %s\n", target)
	return 0
}

func (a *App) uninstallCodex(args []string) int {
	scope := ""
	dryRun := false
	for i := 0; i < len(args); i++ {
		need := func(name string) (string, bool) {
			if i+1 >= len(args) {
				fmt.Fprintf(a.Stderr, "rtk-go uninstall: %s requires a value\n", name)
				return "", false
			}
			i++
			return args[i], true
		}
		switch {
		case args[i] == "codex":
		case args[i] == "--scope":
			v, ok := need("--scope")
			if !ok {
				return 2
			}
			scope = v
		case strings.HasPrefix(args[i], "--scope="):
			scope = strings.TrimPrefix(args[i], "--scope=")
		case args[i] == "--dry-run":
			dryRun = true
		default:
			fmt.Fprintf(a.Stderr, "rtk-go uninstall: unexpected argument %q\n", args[i])
			return 2
		}
	}
	if scope != "project" && scope != "user" {
		fmt.Fprintln(a.Stderr, "rtk-go uninstall codex: --scope must be project or user")
		return 2
	}
	target, err := integrationTarget(scope, "AGENTS.md")
	if err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go uninstall codex: %v\n", err)
		return 1
	}
	existing, modeBits, err := readTarget(target)
	if err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go uninstall codex: %v\n", err)
		return 1
	}
	if !hasBlock(existing) {
		fmt.Fprintf(a.Stdout, "nothing to remove: %s\n", target)
		return 0
	}
	updated := removeBlock(existing)
	if dryRun {
		fmt.Fprintf(a.Stdout, "would remove managed instructions from %s\n", target)
		return 0
	}
	if strings.TrimSpace(updated) == "" {
		if err := backupFile(target, existing); err != nil {
			fmt.Fprintf(a.Stderr, "rtk-go uninstall codex: %v\n", err)
			return 1
		}
		if err := os.Remove(target); err != nil {
			fmt.Fprintf(a.Stderr, "rtk-go uninstall codex: %v\n", err)
			return 1
		}
	} else if err := writeAtomicWithBackup(target, existing, updated, modeBits); err != nil {
		fmt.Fprintf(a.Stderr, "rtk-go uninstall codex: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Stdout, "removed managed instructions from %s\n", target)
	return 0
}

func integrationTarget(scope, name string) (string, error) {
	if scope == "project" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(cwd, name), nil
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(h, ".codex")
	}
	return filepath.Join(home, name), nil
}

func readTarget(path string) (string, os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", 0o644, nil
		}
		return "", 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", 0, fmt.Errorf("%s is a symlink; refusing to follow it", path)
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("%s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	return string(data), info.Mode().Perm(), nil
}

func hasBlock(content string) bool {
	return strings.Contains(content, managedBegin) && strings.Contains(content, managedEnd)
}

func otherManagedBlock(content string) bool {
	lower := strings.ToLower(content)
	for _, marker := range []string{"<!-- rtk:begin -->", "<!-- rtk:begin-->", "<!--rtk:begin"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func appendBlock(existing, block string) string {
	if existing == "" {
		return block
	}
	trimmed := strings.TrimRight(existing, "\n")
	return trimmed + "\n\n" + block
}

func removeBlock(content string) string {
	start := strings.Index(content, managedBegin)
	if start < 0 {
		return content
	}
	end := strings.Index(content[start:], managedEnd)
	if end < 0 {
		return content
	}
	end = start + end + len(managedEnd)
	for end < len(content) && (content[end] == '\r' || content[end] == '\n') {
		end++
	}
	out := strings.TrimRight(content[:start], "\n")
	tail := content[end:]
	tail = strings.TrimLeft(tail, "\r\n")
	if out != "" && tail != "" {
		out += "\n\n"
	}
	result := out + tail
	if strings.HasSuffix(content, "\n") && result != "" && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}

func writeAtomicWithBackup(path, before, content string, mode os.FileMode) error {
	info, err := os.Stat(path)
	if err == nil {
		// Detect an external edit between planning and writing.
		current, readErr := os.ReadFile(path)
		if readErr != nil || string(current) != before {
			return fmt.Errorf("conflict: %s changed after planning; no write performed", path)
		}
		_ = info
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := backupFile(path, before); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".rtk-go-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.WriteString(content); err != nil {
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
	if mode == 0 {
		mode = 0o644
	}
	_ = os.Chmod(name, mode)
	return os.Rename(name, path)
}

func backupFile(path, content string) error {
	if content == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000")
	backup := path + ".rtk-go.bak-" + stamp
	return os.WriteFile(backup, []byte(content), 0o600)
}
