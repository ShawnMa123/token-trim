//go:build !windows

package exec

import (
	"os/exec"
	"syscall"
)

// procTree ties the child and its descendants into one process group.
type procTree struct{ pid int }

func startTree(cmd *exec.Cmd) (*procTree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &procTree{pid: cmd.Process.Pid}, nil
}

func (t *procTree) kill() {
	// The process group id equals the direct child pid. This cannot target an
	// unrelated same-named process.
	_ = syscall.Kill(-t.pid, syscall.SIGKILL)
}

func (t *procTree) close() {}
