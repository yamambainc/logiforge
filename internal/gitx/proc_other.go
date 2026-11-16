//go:build !unix

package gitx

import "os/exec"

// killGroup は、unix でなければ、git 本体だけを kill する（exec.CommandContext の既定）。子のプロセスは、WaitDelay で待つのをやめる。
func killGroup(*exec.Cmd) {}
