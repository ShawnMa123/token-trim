// Package exec runs a child process exactly once with captured or streamed
// stdout while stderr is always drained and mirrored.
package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// CapturePolicy bounds stdout buffering. A nil policy means raw streaming.
type CapturePolicy struct {
	// MemoryLimit is the maximum number of stdout bytes kept in memory.
	MemoryLimit int
	// LineLimit switches to raw when a single line exceeds this length.
	LineLimit int
	// Deadline switches to raw when the process has not finished by then.
	Deadline time.Time
}

// Spec describes one child invocation.
type Spec struct {
	Program string
	Args    []string
	Dir     string
	Env     []string
	Stdin   *os.File
	Stdout  io.Writer
	Stderr  io.Writer

	// Capture buffers stdout until EOF or a limit. nil streams stdout.
	Capture *CapturePolicy
	// MirrorLimit stores up to this many stderr bytes for recovery.
	MirrorLimit int
}

// Result reports the invocation outcome. The child exit status is preserved
// unless a wrapper lifecycle error (incomplete output or broken output pipe)
// makes the result unknowable.
type Result struct {
	ExitCode   int
	Started    bool
	Canceled   bool
	Resolved   string
	SpawnError string

	Stdout         []byte
	StdoutCaptured bool // complete stdout is in Stdout
	StdoutRaw      bool // stdout was streamed; no candidate may be produced
	StdoutBytes    int64

	Stderr         []byte
	StderrComplete bool // mirror holds the complete stderr
	StderrBytes    int64

	// IncompleteOutput means a descendant kept the pipes open past the drain
	// grace period; the output is known to be incomplete.
	IncompleteOutput bool
	// OutputError is a failure writing to the caller's stdout/stderr.
	OutputError error
	Duration    time.Duration
}

// Run executes spec once.
func Run(ctx context.Context, spec Spec) Result {
	start := time.Now()
	res := Result{}
	defer func() { res.Duration = time.Since(start) }()

	if spec.Stdout == nil {
		spec.Stdout = io.Discard
	}
	if spec.Stderr == nil {
		spec.Stderr = io.Discard
	}

	path, code, err := Resolve(spec.Program)
	if err != nil {
		res.ExitCode = code
		res.SpawnError = err.Error()
		return res
	}
	res.Resolved = path

	outR, outW, err := os.Pipe()
	if err != nil {
		res.ExitCode = 125
		res.SpawnError = "create stdout pipe: " + err.Error()
		return res
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		res.ExitCode = 125
		res.SpawnError = "create stderr pipe: " + err.Error()
		return res
	}
	defer outR.Close()
	defer errR.Close()

	cmd := exec.Command(path)
	cmd.Args = append([]string{spec.Program}, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = spec.Stdin
	cmd.Stdout = outW
	cmd.Stderr = errW

	tr, err := startTree(cmd)
	// The parent must close its write ends regardless of start success; the
	// child holds the only remaining handles.
	outW.Close()
	errW.Close()
	if err != nil {
		res.ExitCode = classifyStartError(err)
		res.SpawnError = err.Error()
		return res
	}
	res.Started = true
	defer tr.close()

	outCap := newStdoutCapture(spec.Capture, spec.Stdout)
	errMirror := newMirror(spec.Stderr, spec.MirrorLimit)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		outCap.read(outR)
	}()
	go func() {
		defer wg.Done()
		errMirror.read(errR)
	}()

	// Cancellation: host context or an output write failure tears down the
	// whole tree; it never leaves descendants behind.
	killOnce := sync.Once{}
	kill := func(canceled bool) {
		killOnce.Do(func() {
			tr.kill()
			if canceled {
				res.Canceled = true
			}
		})
	}
	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			kill(true)
		case <-outCap.errCh:
			kill(false)
		case <-errMirror.errCh:
			kill(false)
		case <-stopWatch:
		}
	}()

	waitErr := cmd.Wait()
	close(stopWatch)
	res.ExitCode = exitCode(cmd.ProcessState, waitErr)

	var errMirrorErr error
	drained := waitTimeout(&wg, 2*time.Second)
	if !drained {
		// A descendant still holds the pipes: reclaim the tree, stop waiting,
		// and report incomplete output rather than a reassuring summary.
		tr.kill()
		res.IncompleteOutput = true
		waitTimeout(&wg, 5*time.Second)
	}
	outCap.finish()
	errMirror.finish()

	res.StdoutBytes, res.OutputError = outCap.stats()
	res.Stdout = outCap.data()
	res.StdoutRaw = !outCap.isComplete()
	res.StdoutCaptured = outCap.isComplete()
	res.Stderr = errMirror.data()
	res.StderrComplete = errMirror.isComplete()
	res.StderrBytes, errMirrorErr = errMirror.stats()
	if res.OutputError == nil {
		res.OutputError = errMirrorErr
	}

	if res.IncompleteOutput && res.ExitCode == 0 && !res.Canceled {
		res.ExitCode = 125
	}
	if res.OutputError != nil && res.ExitCode == 0 {
		res.ExitCode = 125
	}
	return res
}

func waitTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func exitCode(state *os.ProcessState, waitErr error) int {
	if state == nil {
		return 125
	}
	code := state.ExitCode()
	if code >= 0 {
		return code
	}
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	if waitErr != nil {
		return 125
	}
	return 125
}

// Resolve maps a program name to an executable path and contract exit code.
func Resolve(program string) (string, int, error) {
	if program == "" || strings.ContainsRune(program, 0) {
		return "", 127, fmt.Errorf("empty or invalid program name")
	}
	path, err := exec.LookPath(program)
	if err != nil {
		return "", classifyStartError(err), fmt.Errorf("%s: %w", program, err)
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", classifyStartError(err), err
	}
	if st.IsDir() {
		return "", 126, fmt.Errorf("%s: is a directory", path)
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".cmd", ".bat", ".ps1":
			return path, 126, fmt.Errorf("unsupported launch form %q: batch and PowerShell scripts are not wrapped; run the original command in your shell", filepath.Base(path))
		}
	}
	return path, 0, nil
}

func classifyStartError(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOENT):
		return 127
	case errors.Is(err, fs.ErrPermission), errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return 126
	default:
		s := strings.ToLower(err.Error())
		if strings.Contains(s, "not found") || strings.Contains(s, "cannot find") {
			return 127
		}
		if strings.Contains(s, "access is denied") || strings.Contains(s, "permission denied") {
			return 126
		}
		return 125
	}
}
