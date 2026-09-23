package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const chatGPTCDN = "https://persistent.oaistatic.com/codex-app-prod"

type system struct {
	goos, goarch, home string
	getenv             func(string) string
	stat               func(string) (os.FileInfo, error)
	run                func(context.Context, []string, string, ...string) (string, error)
	download           func(context.Context, string, string) error
	start              func(string, ...string) error
	startEnv           func([]string, string, ...string) error
	note               func(string)
}

func localSystem(getenv func(string) string) system {
	home, _ := os.UserHomeDir()
	if h := strings.TrimSpace(getenv("HOME")); h != "" {
		home = h
	}
	return system{
		goos: runtime.GOOS, goarch: runtime.GOARCH, home: home, getenv: getenv,
		stat: os.Stat, run: execRun, download: httpDownload, start: startDetached, startEnv: startDetachedEnv,
		note: func(message string) { notify(message) },
	}
}

func supportDir(home, goos string) string {
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "unreal-agent++")
	case "windows":
		return filepath.Join(home, "AppData", "Local", "unreal-agent++")
	default:
		return filepath.Join(home, ".local", "share", "unreal-agent++")
	}
}

func installerURL(goos, goarch string) (string, error) {
	switch goos {
	case "darwin":
		switch goarch {
		case "amd64":
			return chatGPTCDN + "/ChatGPT-latest-x64.dmg", nil
		case "arm64":
			return chatGPTCDN + "/ChatGPT.dmg", nil
		}
	case "windows":
		switch goarch {
		case "amd64":
			return chatGPTCDN + "/ChatGPT-x64.msix", nil
		case "arm64":
			return chatGPTCDN + "/ChatGPT-arm64.msix", nil
		}
	case "linux":
		switch goarch {
		case "amd64":
			return chatGPTCDN + "/linux/deb/latest/chatgpt_amd64.deb", nil
		case "arm64":
			return chatGPTCDN + "/linux/deb/latest/chatgpt_arm64.deb", nil
		}
	}
	return "", fmt.Errorf("ChatGPT desktop is not available for %s/%s; see https://chatgpt.com/download/", goos, goarch)
}

func ensureChatGPT(ctx context.Context, sys system) error {
	if bin, app := findChatGPT(sys); bin != "" {
		return openChatGPT(sys, bin, app)
	}
	if sys.note != nil {
		sys.note("Downloading ChatGPT…")
	}
	installed, err := installChatGPT(ctx, sys)
	if err != nil {
		return err
	}
	bin, app := findChatGPT(sys)
	if bin == "" {
		bin, app = installed, installed
	}
	return openChatGPT(sys, bin, app)
}

func findChatGPT(sys system) (bin, app string) {
	for _, candidate := range candidates(sys) {
		info, err := sys.stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if sys.goos == "darwin" && strings.HasSuffix(candidate, "/Contents/MacOS/ChatGPT") {
			bundle := strings.TrimSuffix(candidate, "/Contents/MacOS/ChatGPT")
			if _, err := sys.stat(filepath.Join(bundle, "Contents", "Resources", "codex")); err != nil {
				continue
			}
			return candidate, bundle
		}
		return candidate, candidate
	}
	return "", ""
}

func candidates(sys system) []string {
	root := supportDir(sys.home, sys.goos)
	switch sys.goos {
	case "darwin":
		return []string{filepath.Join(root, "ChatGPT.app", "Contents", "MacOS", "ChatGPT")}
	case "windows":
		return []string{filepath.Join(root, "ChatGPT", "ChatGPT.exe")}
	default:
		return []string{filepath.Join(root, "usr", "lib", "chatgpt", "ChatGPT")}
	}
}

func openChatGPT(sys system, bin, _ string) error {
	if sys.startEnv == nil {
		return errors.New("no process starter")
	}
	root := supportDir(sys.home, sys.goos)
	data := filepath.Join(root, "desktop")
	codex := filepath.Join(root, "codex")
	if err := os.MkdirAll(data, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(codex, 0o755); err != nil {
		return err
	}
	// Launch the private binary. LaunchServices would hand the same bundle id back to /Applications/ChatGPT.app.
	env := append(os.Environ(), "CODEX_HOME="+codex, "CODEX_ELECTRON_USER_DATA_PATH="+data)
	return sys.startEnv(env, bin, "--user-data-dir="+data)
}

func installChatGPT(ctx context.Context, sys system) (string, error) {
	switch sys.goos {
	case "darwin":
		return installDarwin(ctx, sys)
	case "windows":
		return installWindows(ctx, sys)
	case "linux":
		return installLinux(ctx, sys)
	default:
		_, err := installerURL(sys.goos, sys.goarch)
		return "", err
	}
}

func installDarwin(ctx context.Context, sys system) (string, error) {
	url, err := installerURL("darwin", sys.goarch)
	if err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp("", "unreal-agent-chatgpt-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	dmg := filepath.Join(scratch, "ChatGPT.dmg")
	if err := sys.download(ctx, url, dmg); err != nil {
		return "", err
	}
	mount := filepath.Join(scratch, "mount")
	if err := os.Mkdir(mount, 0o755); err != nil {
		return "", err
	}
	if _, err := sys.run(ctx, nil, "hdiutil", "attach", "-nobrowse", "-readonly", "-mountpoint", mount, dmg); err != nil {
		return "", err
	}
	defer sys.run(context.Background(), nil, "hdiutil", "detach", mount)
	source := filepath.Join(mount, "ChatGPT.app")
	if _, err := sys.run(ctx, nil, "codesign", "--verify", "--deep", "--strict", source); err != nil {
		return "", err
	}
	if _, err := sys.stat(filepath.Join(source, "Contents", "Resources", "codex")); err != nil {
		return "", errors.New("installer contains ChatGPT Classic, not the Codex desktop app")
	}
	applications := supportDir(sys.home, "darwin")
	if err := os.MkdirAll(applications, 0o755); err != nil {
		return "", err
	}
	destination := filepath.Join(applications, "ChatGPT.app")
	if _, err := sys.stat(destination); err == nil {
		return "", fmt.Errorf("%s already exists but is not usable; move it aside", destination)
	}
	staged := filepath.Join(applications, fmt.Sprintf(".unreal-agent-chatgpt-%d.app", os.Getpid()))
	defer os.RemoveAll(staged)
	if _, err := sys.run(ctx, nil, "ditto", source, staged); err != nil {
		return "", err
	}
	if err := os.Rename(staged, destination); err != nil {
		return "", err
	}
	return filepath.Join(destination, "Contents", "MacOS", "ChatGPT"), nil
}

func installWindows(ctx context.Context, sys system) (string, error) {
	if exe := windowsPackage(ctx, sys); exe != "" {
		return exe, nil
	}
	url, err := installerURL("windows", sys.goarch)
	if err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp("", "unreal-agent-chatgpt-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	file := filepath.Join(scratch, "ChatGPT.msix")
	if err := sys.download(ctx, url, file); err != nil {
		return "", err
	}
	env := append(os.Environ(), "UNREAL_CHATGPT_INSTALLER="+file)
	if _, err := sys.run(ctx, env, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"$ErrorActionPreference = 'Stop'; Add-AppxPackage -Path $env:UNREAL_CHATGPT_INSTALLER"); err != nil {
		return "", err
	}
	if exe := windowsPackage(ctx, sys); exe != "" {
		return exe, nil
	}
	return "", errors.New("ChatGPT installed but its desktop executable was not found")
}

func windowsPackage(ctx context.Context, sys system) string {
	out, err := sys.run(ctx, nil, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", windowsFindScript)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

const windowsFindScript = `$ErrorActionPreference = 'Stop'
Get-AppxPackage | Where-Object { $_.Name -match 'OpenAI|ChatGPT|Codex' } | ForEach-Object {
  $pkg = $_
  $manifest = Get-AppxPackageManifest -Package $pkg.PackageFullName
  foreach ($entry in $manifest.Package.Applications.Application) {
    if ($entry.Executable) {
      $exe = Join-Path $pkg.InstallLocation $entry.Executable
      if ((Test-Path -LiteralPath $exe) -and (Test-Path -LiteralPath (Join-Path (Split-Path $exe) 'resources/codex.exe'))) { Write-Output $exe }
    }
  }
}`

func installLinux(ctx context.Context, sys system) (string, error) {
	url, err := installerURL("linux", sys.goarch)
	if err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp("", "unreal-agent-chatgpt-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)
	deb := filepath.Join(scratch, "chatgpt.deb")
	if err := sys.download(ctx, url, deb); err != nil {
		return "", err
	}
	dest := supportDir(sys.home, "linux")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	if _, err := sys.run(ctx, nil, "dpkg-deb", "-x", deb, dest); err != nil {
		return "", err
	}
	bin := filepath.Join(dest, "usr", "lib", "chatgpt", "ChatGPT")
	if _, err := sys.stat(bin); err != nil {
		return "", fmt.Errorf("unpacked ChatGPT has no %s", bin)
	}
	return bin, nil
}

func execRun(ctx context.Context, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func httpDownload(ctx context.Context, url, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed (%s): %s", response.Status, url)
	}
	if strings.Contains(response.Header.Get("Content-Type"), "text/html") {
		return fmt.Errorf("installer URL returned HTML: %s", url)
	}
	partial := dest + ".part"
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(partial)
		return errors.Join(copyErr, closeErr)
	}
	info, err := os.Stat(partial)
	if err != nil || info.Size() == 0 {
		os.Remove(partial)
		return errors.New("downloaded installer is empty")
	}
	return os.Rename(partial, dest)
}
