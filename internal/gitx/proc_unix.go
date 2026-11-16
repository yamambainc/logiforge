//go:build unix

package gitx

import (
	"os/exec"
	"syscall"
)

// killGroup は、git を新しいプロセスのグループで動かし、止めるときはグループごと kill する。
// git は、git-remote-https などの子のプロセスを起こす。git 本体だけを kill すると、子がパイプを持ったまま残り、待ちが戻らない。
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
