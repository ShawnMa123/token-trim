package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var helperExe string

func TestMain(m *testing.M) {
	if mode := os.Getenv("RTK_EXEC_HELPER"); mode != "" {
		helperMain(mode)
		return
	}
	helperExe, _ = os.Executable()
	os.Exit(m.Run())
}

func helperMain(mode string) {
	self, _ := os.Executable()
	helperExe = self
	switch mode {
	case "args":
		wd, _ := os.Getwd()
		stdin := make([]byte, 64)
		n, _ := os.Stdin.Read(stdin)
		out := map[string]any{
			"argv":  os.Args[1:],
			"cwd":   wd,
			"env":   os.Getenv("SENTINEL"),
			"stdin": string(stdin[:n]),
		}
		_ = json.NewEncoder(os.Stdout).Encode(out)
		os.Exit(0)
	case "both":
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			block := bytes.Repeat([]byte("o"), 64*1024)
			for range 160 {
				if _, err := os.Stdout.Write(block); err != nil {
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			block := bytes.Repeat([]byte("e"), 64*1024)
			for range 160 {
				if _, err := os.Stderr.Write(block); err != nil {
					return
				}
			}
		}()
		wg.Wait()
		os.Exit(0)
	case "binary":
		_, _ = os.Stdout.Write([]byte{0, 1, 2, 0xff, 0xfe, 'x'})
		os.Exit(0)
	case "stderr":
		_, _ = os.Stdout.WriteString("out")
		_, _ = os.Stderr.WriteString(os.Getenv("RTK_STDERR_TEXT"))
		os.Exit(0)
	case "slow":
		_, _ = os.Stdout.WriteString("prefix")
		time.Sleep(250 * time.Millisecond)
		_, _ = os.Stdout.WriteString("suffix")
		os.Exit(0)
	case "longline":
		_, _ = os.Stdout.WriteString(strings.Repeat("x", 64))
		os.Exit(0)
	case "exit":
		code := 0
		fmt.Sscan(os.Getenv("RTK_EXIT_CODE"), &code)
		_, _ = os.Stdout.WriteString("bye")
		os.Exit(code)
	case "sleep":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "marker":
		time.Sleep(1200 * time.Millisecond)
		_ = os.WriteFile(os.Getenv("RTK_MARKER"), []byte("alive"), 0o600)
		time.Sleep(10 * time.Second)
		os.Exit(0)
	case "tree":
		cmd := exec.Command(helperExe)
		cmd.Env = append(os.Environ(), "RTK_EXEC_HELPER=marker", "RTK_MARKER="+os.Getenv("RTK_MARKER"))
		cmd.Stdout = os.Stdout
		if err := cmd.Start(); err != nil {
			os.Exit(3)
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "holdpipe":
		cmd := exec.Command(helperExe)
		cmd.Env = append(os.Environ(), "RTK_EXEC_HELPER=sleep")
		cmd.Stdout = os.Stdout
		if err := cmd.Start(); err != nil {
			os.Exit(3)
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = os.Stdout.WriteString("parent-done")
		os.Exit(0)
	}
	os.Exit(9)
}

func TestArgvCwdEnvStdinFidelity(t *testing.T) {
	dir := t.TempDir()
	stdinFile := filepath.Join(dir, "stdin.txt")
	if err := os.WriteFile(stdinFile, []byte("input-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(stdinFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var out, errOut bytes.Buffer
	wantArgs := []string{"a b", "", "中文路径", "a;b", "x|y", "--format=json", "-v"}
	res := Run(context.Background(), Spec{
		Program: helperExe,
		Args:    wantArgs,
		Dir:     dir,
		Env:     append(os.Environ(), "RTK_EXEC_HELPER=args", "SENTINEL=value"),
		Stdin:   f,
		Stdout:  &out,
		Stderr:  &errOut,
	})
	if res.ExitCode != 0 || !res.Started {
		t.Fatalf("exit=%d spawn=%q stderr=%q", res.ExitCode, res.SpawnError, errOut.String())
	}
	var got struct {
		Argv  []string `json:"argv"`
		Cwd   string   `json:"cwd"`
		Env   string   `json:"env"`
		Stdin string   `json:"stdin"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", out.String(), err)
	}
	if len(got.Argv) != len(wantArgs) {
		t.Fatalf("argv = %q", got.Argv)
	}
	for i := range wantArgs {
		if got.Argv[i] != wantArgs[i] {
			t.Fatalf("argv[%d] = %q want %q", i, got.Argv[i], wantArgs[i])
		}
	}
	realDir, _ := filepath.EvalSymlinks(dir)
	realGot, _ := filepath.EvalSymlinks(got.Cwd)
	if !strings.EqualFold(realDir, realGot) {
		t.Fatalf("cwd = %q want %q", got.Cwd, dir)
	}
	if got.Env != "value" || got.Stdin != "input-data" {
		t.Fatalf("env=%q stdin=%q", got.Env, got.Stdin)
	}
}

func TestBothPipesExceedCapacity(t *testing.T) {
	var out, errOut bytes.Buffer
	res := Run(context.Background(), Spec{
		Program:     helperExe,
		Env:         append(os.Environ(), "RTK_EXEC_HELPER=both"),
		Stdout:      &out,
		Stderr:      &errOut,
		MirrorLimit: 64 * 1024 * 1024,
	})
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d", res.ExitCode)
	}
	if out.Len() != 10*1024*1024 || errOut.Len() != 10*1024*1024 {
		t.Fatalf("stdout=%d stderr=%d", out.Len(), errOut.Len())
	}
	if !res.StderrComplete || res.StderrBytes != 10*1024*1024 {
		t.Fatalf("stderr mirror complete=%v bytes=%d", res.StderrComplete, res.StderrBytes)
	}
}

func TestRawBinaryFidelity(t *testing.T) {
	var out bytes.Buffer
	res := Run(context.Background(), Spec{
		Program: helperExe,
		Env:     append(os.Environ(), "RTK_EXEC_HELPER=binary"),
		Stdout:  &out,
	})
	want := []byte{0, 1, 2, 0xff, 0xfe, 'x'}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("got %v want %v", out.Bytes(), want)
	}
	if !res.StdoutRaw {
		t.Fatal("expected raw streaming")
	}
}

func TestCaptureComplete(t *testing.T) {
	var out bytes.Buffer
	res := Run(context.Background(), Spec{
		Program: helperExe,
		Env:     append(os.Environ(), "RTK_EXEC_HELPER=slow"),
		Stdout:  &out,
		Capture: &CapturePolicy{
			MemoryLimit: 1 << 20,
			LineLimit:   1 << 20,
			Deadline:    time.Now().Add(5 * time.Second),
		},
	})
	if !res.StdoutCaptured || res.StdoutRaw {
		t.Fatalf("captured=%v raw=%v", res.StdoutCaptured, res.StdoutRaw)
	}
	if string(res.Stdout) != "prefixsuffix" {
		t.Fatalf("stdout = %q", res.Stdout)
	}
	if out.Len() != 0 {
		t.Fatalf("captured output must not be emitted before filtering: %q", out.String())
	}
}

func TestDeadlineTransitionsRawExactlyOnce(t *testing.T) {
	var out bytes.Buffer
	res := Run(context.Background(), Spec{
		Program: helperExe,
		Env:     append(os.Environ(), "RTK_EXEC_HELPER=slow"),
		Stdout:  &out,
		Capture: &CapturePolicy{
			MemoryLimit: 1 << 20,
			LineLimit:   1 << 20,
			Deadline:    time.Now().Add(80 * time.Millisecond),
		},
	})
	if res.StdoutRaw {
		// expected
	} else {
		t.Fatal("expected raw transition after deadline")
	}
	if out.String() != "prefixsuffix" {
		t.Fatalf("streamed output = %q", out.String())
	}
	if res.StdoutBytes != int64(len("prefixsuffix")) {
		t.Fatalf("counted bytes = %d", res.StdoutBytes)
	}
}

func TestMemoryLimitTransition(t *testing.T) {
	var out bytes.Buffer
	res := Run(context.Background(), Spec{
		Program: helperExe,
		Env:     append(os.Environ(), "RTK_EXEC_HELPER=both"),
		Stdout:  &out,
		Capture: &CapturePolicy{MemoryLimit: 1024, LineLimit: 1 << 20},
	})
	if !res.StdoutRaw || out.Len() != 10*1024*1024 {
		t.Fatalf("raw=%v emitted=%d", res.StdoutRaw, out.Len())
	}
}

func TestLineLimitTransition(t *testing.T) {
	var out bytes.Buffer
	res := Run(context.Background(), Spec{
		Program: helperExe,
		Env:     append(os.Environ(), "RTK_EXEC_HELPER=longline"),
		Stdout:  &out,
		Capture: &CapturePolicy{MemoryLimit: 1 << 20, LineLimit: 10},
	})
	if !res.StdoutRaw {
		t.Fatal("long line should force raw")
	}
	if out.String() != strings.Repeat("x", 64) {
		t.Fatalf("emitted %q", out.String())
	}
}

func TestExitStatusPreserved(t *testing.T) {
	var out bytes.Buffer
	res := Run(context.Background(), Spec{
		Program: helperExe,
		Env:     append(os.Environ(), "RTK_EXEC_HELPER=exit", "RTK_EXIT_CODE=7"),
		Stdout:  &out,
	})
	if res.ExitCode != 7 {
		t.Fatalf("exit = %d", res.ExitCode)
	}
}

func TestSpawnNotFound(t *testing.T) {
	res := Run(context.Background(), Spec{Program: "definitely-not-a-real-program-xyz", Stdout: &bytes.Buffer{}})
	if res.ExitCode != 127 || res.Started {
		t.Fatalf("exit=%d started=%v spawn=%q", res.ExitCode, res.Started, res.SpawnError)
	}
}

func TestStderrMirrorOverflow(t *testing.T) {
	text := "0123456789"
	var out, errOut bytes.Buffer
	res := Run(context.Background(), Spec{
		Program:     helperExe,
		Env:         append(os.Environ(), "RTK_EXEC_HELPER=stderr", "RTK_STDERR_TEXT="+text),
		Stdout:      &out,
		Stderr:      &errOut,
		MirrorLimit: 5,
	})
	if errOut.String() != text {
		t.Fatalf("stderr emitted %q", errOut.String())
	}
	if res.StderrComplete {
		t.Fatal("mirror must report incomplete")
	}
	if res.StderrBytes != int64(len(text)) {
		t.Fatalf("stderr bytes = %d", res.StderrBytes)
	}
}

func TestCancellationKillsTree(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	done := make(chan Result, 1)
	go func() {
		done <- Run(ctx, Spec{
			Program: helperExe,
			Env:     append(os.Environ(), "RTK_EXEC_HELPER=tree", "RTK_MARKER="+marker),
			Stdout:  &out,
		})
	}()
	time.Sleep(500 * time.Millisecond)
	cancel()
	select {
	case res := <-done:
		if !res.Canceled {
			t.Fatalf("expected canceled result: %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not return")
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("descendant survived cancellation and wrote its marker")
	}
}

func TestDescendantHoldingPipeReportsIncomplete(t *testing.T) {
	var out bytes.Buffer
	start := time.Now()
	res := Run(context.Background(), Spec{
		Program: helperExe,
		Env:     append(os.Environ(), "RTK_EXEC_HELPER=holdpipe"),
		Stdout:  &out,
	})
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Fatalf("drain wait took %v", elapsed)
	}
	if !res.IncompleteOutput {
		t.Fatal("expected incomplete output")
	}
	if res.ExitCode != 125 {
		t.Fatalf("exit = %d, want 125 for child status 0 with incomplete output", res.ExitCode)
	}
	if !strings.Contains(out.String(), "parent-done") {
		t.Fatalf("emitted %q", out.String())
	}
}
