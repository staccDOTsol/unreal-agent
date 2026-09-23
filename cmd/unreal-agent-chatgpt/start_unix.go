//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
