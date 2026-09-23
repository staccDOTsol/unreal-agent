//go:build windows

package main

import (
	"os"
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
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	return cmd.Start()
}

func notify(message string) {}

func notifyError(err error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.MessageBox]::Show($env:UNREAL_CHATGPT_ERROR, 'unreal-agent++')")
	cmd.Env = append(os.Environ(), "UNREAL_CHATGPT_ERROR="+err.Error())
	_ = cmd.Run()
}
