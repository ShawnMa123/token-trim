package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShawnMa123/token-trim/go/internal/recovery"
)

func writeTestConfig(t *testing.T, dataDir string) string {
	t.Helper()
	body := map[string]any{
		"schema_version": 1,
		"storage":        map[string]any{"data_dir": dataDir},
	}
	data, _ := json.Marshal(body)
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RTK_GO_CONFIG", p)
	t.Setenv("RTK_GO_NO_STORE", "")
	t.Setenv("RTK_GO_DISABLE", "")
	return p
}

func TestParseExecArgs(t *testing.T) {
	opts, argv, err := parseExecArgs([]string{"--raw", "--profile", "safe", "--", "git", "status"})
	if err != nil || !opts.raw || opts.profile != "safe" || strings.Join(argv, " ") != "git status" {
		t.Fatalf("opts=%+v argv=%q err=%v", opts, argv, err)
	}
	if _, _, err := parseExecArgs([]string{"--encoding", "gb18030", "--", "x"}); err == nil {
		t.Fatal("gb18030 must be rejected")
	}
	if _, _, err := parseExecArgs([]string{"--wat", "--", "x"}); err == nil {
		t.Fatal("unknown option must be rejected")
	}
	if _, _, err := parseExecArgs([]string{"--"}); err == nil {
		t.Fatal("missing program must be rejected")
	}
	opts, argv, err = parseExecArgs([]string{"git", "-v"})
	if err != nil || strings.Join(argv, " ") != "git -v" || opts.raw {
		t.Fatalf("shorthand parse: %+v %q %v", opts, argv, err)
	}
}

func runCLI(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Run(args, os.Stdin, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVersionJSON(t *testing.T) {
	code, out, _ := runCLI("version", "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("json: %v (%q)", err, out)
	}
	if v["schema_version"] != float64(1) {
		t.Fatalf("payload = %v", v)
	}
}

func TestHookIsJSONNoop(t *testing.T) {
	code, out, errOut := runCLI("hook", "codex")
	if code != 0 || strings.TrimSpace(out) != "{}" || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestInstructionsInstallUninstall(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	target := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(target, []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runCLI("init", "codex", "--mode", "instructions", "--scope", "project", "--dry-run"); code != 0 || !strings.Contains(out, "would update") {
		t.Fatalf("dry run code=%d out=%q err=%q", code, out, errOut)
	}
	data, _ := os.ReadFile(target)
	if strings.Contains(string(data), managedBegin) {
		t.Fatal("dry run wrote the file")
	}
	if code, _, errOut := runCLI("init", "codex", "--mode", "instructions", "--scope", "project"); code != 0 {
		t.Fatalf("install code=%d err=%q", code, errOut)
	}
	data, _ = os.ReadFile(target)
	if !strings.Contains(string(data), managedBegin) || !strings.Contains(string(data), "existing") {
		t.Fatalf("installed content = %q", data)
	}
	if code, _, _ := runCLI("init", "codex", "--mode", "instructions", "--scope", "project"); code != 0 {
		t.Fatal("second install failed")
	}
	data, _ = os.ReadFile(target)
	if strings.Count(string(data), managedBegin) != 1 {
		t.Fatal("duplicate managed block")
	}
	if code, _, errOut := runCLI("uninstall", "codex", "--scope", "project"); code != 0 {
		t.Fatalf("uninstall code=%d err=%q", code, errOut)
	}
	data, _ = os.ReadFile(target)
	if strings.Contains(string(data), managedBegin) || !strings.Contains(string(data), "existing") {
		t.Fatalf("after uninstall = %q", data)
	}
	if code, _, _ := runCLI("uninstall", "codex", "--scope", "project"); code != 0 {
		t.Fatal("second uninstall failed")
	}
}

func TestHooksModeRefused(t *testing.T) {
	code, out, errOut := runCLI("init", "codex", "--mode", "hooks", "--scope", "project", "--dry-run")
	if code != 1 || out != "" || !strings.Contains(errOut, "not enabled") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestRecallPaginationAndRaw(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	writeTestConfig(t, root)
	store := recovery.New(root, 256, 24)
	if err := store.Prepare(); err != nil {
		t.Fatal(err)
	}
	id, _ := recovery.NewID()
	res, ok := store.Acquire(id)
	if !ok {
		t.Fatal("acquire")
	}
	body := "one\ntwo\nthree\n"
	if _, err := res.Commit(id, []byte(body), []byte("err\n"), 0, "utf-8"); err != nil {
		t.Fatal(err)
	}
	res.Release()

	code, out, errOut := runCLI("recall", id, "--stream", "stdout", "--offset", "2", "--limit", "1")
	if code != 0 || !strings.Contains(out, "[stdout] lines 2-2 of 3") || !strings.Contains(out, "two\n") || strings.Contains(out, "one") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, errOut = runCLI("recall", id, "--stream", "stdout", "--raw")
	if code != 0 || out != body {
		t.Fatalf("raw code=%d out=%q err=%q", code, out, errOut)
	}
	code, _, errOut = runCLI("recall", id, "--raw")
	if code != 2 || !strings.Contains(errOut, "single") {
		t.Fatalf("both+raw code=%d err=%q", code, errOut)
	}
	code, _, _ = runCLI("recall", id, "--stream", "stdout", "--offset", "99")
	if code != 0 {
		t.Fatalf("out-of-range offset code=%d", code)
	}
}

func TestRecallMissingAndTraversal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	writeTestConfig(t, root)
	code, _, errOut := runCLI("recall", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if code != 1 || !strings.Contains(errOut, "not found") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	code, _, _ = runCLI("recall", "../etc/passwd")
	if code != 1 {
		t.Fatalf("traversal code=%d", code)
	}
}

func TestConfigShowJSON(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	writeTestConfig(t, root)
	code, out, _ := runCLI("config", "show", "--format", "json")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	var payload struct {
		Config struct {
			SchemaVersion int `json:"schema_version"`
			Storage       struct {
				DataDir string `json:"data_dir"`
			} `json:"storage"`
		} `json:"config"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Config.SchemaVersion != 1 || payload.Config.Storage.DataDir != root {
		t.Fatalf("payload=%+v", payload)
	}
}
