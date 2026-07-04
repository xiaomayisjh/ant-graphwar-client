//go:build !windows

package backend

import "os/exec"

func hideChildWindow(cmd *exec.Cmd) {}
