//go:build darwin

package main

import "os/exec"

func notify(message string) {
	_ = exec.Command("osascript", "-e", `display notification "`+escapeAppleScript(message)+`" with title "unreal-agent++"`).Start()
}

func notifyError(err error) {
	_ = exec.Command("osascript", "-e", `display dialog "`+escapeAppleScript(err.Error())+`" with title "unreal-agent++" buttons {"OK"} default button "OK"`).Run()
}

func escapeAppleScript(text string) string {
	out := make([]byte, 0, len(text))
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\\', '"':
			out = append(out, '\\', text[i])
		case '\n', '\r':
			out = append(out, ' ')
		default:
			out = append(out, text[i])
		}
	}
	return string(out)
}
