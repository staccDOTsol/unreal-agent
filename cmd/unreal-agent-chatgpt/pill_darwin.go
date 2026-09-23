//go:build darwin

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func startPill(sys system) {
	exe, err := os.Executable()
	if err != nil || sys.start == nil {
		return
	}
	_ = sys.start(exe, "--pill")
}

func watchPill(ctx context.Context, getenv func(string) string) error {
	home, _ := os.UserHomeDir()
	if h := strings.TrimSpace(getenv("HOME")); h != "" {
		home = h
	}
	root := supportDir(home, "darwin")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	pidPath := filepath.Join(root, "pill.pid")
	if pid, err := os.ReadFile(pidPath); err == nil {
		if other, err := strconv.Atoi(strings.TrimSpace(string(pid))); err == nil && other != os.Getpid() && processAlive(other) {
			return nil
		}
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return err
	}
	defer os.Remove(pidPath)
	status := filepath.Join(root, "pill.txt")
	script := filepath.Join(root, "pill.js")
	if err := os.WriteFile(script, []byte(pillScript), 0o644); err != nil {
		return err
	}
	cmd := exec.Command("osascript", "-l", "JavaScript", script, status)
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}()
	var misses int
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	write := func() {
		state := priceSession(filepath.Join(root, "codex"), openRouterCatalog())
		if len(state.Receipt) > 0 {
			_ = os.WriteFile(filepath.Join(root, "receipts.jsonl"), state.Receipt, 0o644)
		}
		_ = os.WriteFile(status, []byte(state.text()), 0o644)
	}
	write()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			write()
			if chatCopyRunning(root) {
				misses = 0
				continue
			}
			misses++
			if misses >= 60 {
				return nil
			}
		}
	}
}

func chatCopyRunning(root string) bool {
	out, err := exec.Command("ps", "-ax", "-o", "command=").Output()
	return err == nil && strings.Contains(string(out), filepath.Join(root, "ChatGPT.app"))
}

func processAlive(pid int) bool {
	err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "pid=").Run()
	return err == nil
}

const pillScript = `
ObjC.import('Cocoa');
function run(argv) {
  var app = $.NSApplication.sharedApplication;
  app.setActivationPolicy($.NSApplicationActivationPolicyAccessory);
  var frame = $.NSScreen.mainScreen.visibleFrame;
  var width = 620, height = 56;
  var win = $.NSWindow.alloc.initWithContentRectStyleMaskBackingDefer(
    $.NSMakeRect(frame.origin.x + frame.size.width - width - 20, frame.origin.y + 96, width, height),
    $.NSWindowStyleMaskBorderless, $.NSBackingStoreBuffered, false);
  win.setLevel($.NSFloatingWindowLevel);
  win.setOpaque(false);
  win.setHasShadow(true);
  win.setMovableByWindowBackground(true);
  win.setBackgroundColor($.NSColor.colorWithCalibratedRedGreenBlueAlpha(0.910, 0.365, 0.110, 0.96));
  function label(y, size, bold) {
    var field = $.NSTextField.alloc.initWithFrame($.NSMakeRect(14, y, width - 28, size + 6));
    field.setBezeled(false);
    field.setDrawsBackground(false);
    field.setEditable(false);
    field.setSelectable(false);
    field.setTextColor($.NSColor.whiteColor);
    field.setFont(bold ? $.NSFont.boldSystemFontOfSize(size) : $.NSFont.systemFontOfSize(size));
    win.contentView.addSubview(field);
    return field;
  }
  var field = label(28, 13, true);
  var sub = label(8, 11, false);
  field.setStringValue('Saved —');
  sub.setStringValue('…');
  win.orderFrontRegardless;
  var path = argv[0];
  $.NSTimer.scheduledTimerWithTimeIntervalRepeatsBlock(0.5, true, function() {
    var text = $.NSString.stringWithContentsOfFileEncodingError(path, $.NSUTF8StringEncoding, null);
    if (!text) return;
    var parts = String(text.stringByTrimmingCharactersInSet($.NSCharacterSet.whitespaceAndNewlineCharacterSet)).split('\n');
    field.setStringValue(parts[0] || 'Saved —');
    sub.setStringValue(parts[1] || '…');
  });
  app.run();
}
`
