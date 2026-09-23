package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyLaunchAndBills(t *testing.T) {
	mode, rest, launch, err := classify(nil)
	if err != nil || !launch || mode != "" || len(rest) != 0 {
		t.Fatalf("empty: mode=%q rest=%v launch=%v err=%v", mode, rest, launch, err)
	}
	mode, rest, launch, err = classify([]string{"-psn_0_123"})
	if err != nil || !launch {
		t.Fatalf("finder: launch=%v err=%v", launch, err)
	}
	mode, rest, launch, err = classify([]string{"--credits", "-p", "hello"})
	if err != nil || launch || mode != "credits" || strings.Join(rest, " ") != "-p hello" {
		t.Fatalf("credits: mode=%q rest=%v launch=%v err=%v", mode, rest, launch, err)
	}
	if _, _, _, err := classify([]string{"--sub", "--credits"}); err == nil {
		t.Fatal("expected mutually exclusive")
	}
	var stderr strings.Builder
	if code := run([]string{"--sub", "--credits"}, os.Getenv, os.Setenv, io.Discard, &stderr); code != 1 || !strings.Contains(stderr.String(), "mutually exclusive") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestConfigureBill(t *testing.T) {
	home := t.TempDir()
	auth := filepath.Join(home, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(auth), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auth, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HOME": home}
	getenv := func(k string) string { return env[k] }
	setenv := func(k, v string) error { env[k] = v; return nil }
	if err := configureBill("", getenv, setenv, fileExists); err != nil {
		t.Fatal(err)
	}
	if env["UNREAL_HARNESS_LLM_PROVIDER"] != "openai-codex" || env["OPENAI_CODEX_AUTH_FILE"] != auth {
		t.Fatalf("sub env = %#v", env)
	}

	env = map[string]string{"HOME": t.TempDir(), "OPENAI_API_KEY": "sk-real"}
	if err := configureBill("", getenv, setenv, fileExists); err != nil {
		t.Fatal(err)
	}
	if env["UNREAL_HARNESS_LLM_PROVIDER"] != "openai" {
		t.Fatalf("credits env = %#v", env)
	}

	env = map[string]string{"HOME": home, "OPENAI_API_KEY": "sk-real"}
	if err := configureBill("credits", getenv, setenv, fileExists); err != nil {
		t.Fatal(err)
	}
	if env["UNREAL_HARNESS_LLM_PROVIDER"] != "openai" || env["OPENAI_API_KEY"] != "sk-real" {
		t.Fatalf("forced credits = %#v", env)
	}

	env = map[string]string{"OPENAI_API_KEY": "sk-openzoo", "HOME": t.TempDir()}
	if err := configureBill("", getenv, setenv, fileExists); err == nil {
		t.Fatal("placeholder key counted as credits")
	}
}

func TestInstallerURL(t *testing.T) {
	url, err := installerURL("darwin", "arm64")
	if err != nil || !strings.HasSuffix(url, "/ChatGPT.dmg") {
		t.Fatal(url, err)
	}
	url, err = installerURL("windows", "amd64")
	if err != nil || !strings.HasSuffix(url, "/ChatGPT-x64.msix") {
		t.Fatal(url, err)
	}
	url, err = installerURL("linux", "arm64")
	if err != nil || !strings.HasSuffix(url, "/chatgpt_arm64.deb") {
		t.Fatal(url, err)
	}
}

func TestOpenInstalledChatGPT(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, "Applications", "ChatGPT.app")
	bin := filepath.Join(app, "Contents", "MacOS", "ChatGPT")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(app, "Contents", "Resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Resources", "codex"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var opened []string
	sys := system{
		goos: "darwin", goarch: "arm64", home: home,
		getenv: func(string) string { return "" },
		stat: func(path string) (os.FileInfo, error) {
			if strings.HasPrefix(path, "/Applications") {
				return nil, os.ErrNotExist
			}
			return os.Stat(path)
		},
		run: func(_ context.Context, _ []string, name string, args ...string) (string, error) {
			opened = append([]string{name}, args...)
			return "", nil
		},
		download: func(context.Context, string, string) error { t.Fatal("downloaded an installed app"); return nil },
	}
	if err := ensureChatGPT(context.Background(), sys); err != nil {
		t.Fatal(err)
	}
	if strings.Join(opened, " ") != "open -n "+app {
		t.Fatalf("opened %q", strings.Join(opened, " "))
	}
}

func TestDownloadThenOpenChatGPT(t *testing.T) {
	home := t.TempDir()
	var downloaded string
	var commands []string
	sys := system{
		goos: "darwin", goarch: "arm64", home: home,
		getenv: func(string) string { return "" },
		stat: func(path string) (os.FileInfo, error) {
			if strings.HasPrefix(path, "/Applications") {
				return nil, os.ErrNotExist
			}
			return os.Stat(path)
		},
		download: func(_ context.Context, url, dest string) error {
			downloaded = url
			return os.WriteFile(dest, []byte("dmg"), 0o644)
		},
		run: func(_ context.Context, _ []string, name string, args ...string) (string, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			if name == "hdiutil" && args[0] == "attach" {
				mount := args[4]
				base := filepath.Join(mount, "ChatGPT.app", "Contents")
				if err := os.MkdirAll(filepath.Join(base, "Resources"), 0o755); err != nil {
					return "", err
				}
				if err := os.WriteFile(filepath.Join(base, "Resources", "codex"), []byte("x"), 0o644); err != nil {
					return "", err
				}
			}
			if name == "ditto" {
				dest := args[1]
				base := filepath.Join(dest, "Contents")
				if err := os.MkdirAll(filepath.Join(base, "MacOS"), 0o755); err != nil {
					return "", err
				}
				if err := os.MkdirAll(filepath.Join(base, "Resources"), 0o755); err != nil {
					return "", err
				}
				if err := os.WriteFile(filepath.Join(base, "MacOS", "ChatGPT"), []byte("x"), 0o755); err != nil {
					return "", err
				}
				if err := os.WriteFile(filepath.Join(base, "Resources", "codex"), []byte("x"), 0o644); err != nil {
					return "", err
				}
			}
			return "", nil
		},
	}
	if err := ensureChatGPT(context.Background(), sys); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(downloaded, "/ChatGPT.dmg") {
		t.Fatal(downloaded)
	}
	joined := strings.Join(commands, "\n")
	if !strings.Contains(joined, "hdiutil attach") || !strings.Contains(joined, "codesign --verify") || !strings.Contains(joined, "open -n") {
		t.Fatal(joined)
	}
}
