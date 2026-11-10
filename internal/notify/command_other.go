//go:build !windows

package notify

import "os/exec"

func prepareCommand(cmd *exec.Cmd) {}
