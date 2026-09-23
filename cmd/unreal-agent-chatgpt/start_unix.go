//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

func startDetached(name string, args ...string) error {
	return startDetachedEnv(nil, name, args...)
}

func startDetachedEnv(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
