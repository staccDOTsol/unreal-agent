package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func configureBill(mode string, getenv func(string) string, setenv func(string, string) error, stat func(string) error) error {
	if mode == "" && strings.TrimSpace(getenv("UNREAL_HARNESS_LLM_PROVIDER")) != "" {
		return nil
	}
	if mode != "credits" && (strings.TrimSpace(getenv("OPENAI_CODEX_ACCESS_TOKEN")) != "" || strings.TrimSpace(getenv("OPENAI_CODEX_ACCOUNT_ID")) != "") {
		return setenv("UNREAL_HARNESS_LLM_PROVIDER", "openai-codex")
	}
	auth := authFile(getenv)
	if mode != "credits" && auth != "" && stat(auth) == nil {
		if err := setenv("UNREAL_HARNESS_LLM_PROVIDER", "openai-codex"); err != nil {
			return err
		}
		if strings.TrimSpace(getenv("OPENAI_CODEX_AUTH_FILE")) == "" {
			return setenv("OPENAI_CODEX_AUTH_FILE", auth)
		}
		return nil
	}
	if mode == "sub" {
		return fmt.Errorf("no ChatGPT subscription at %s; sign in, or pass --credits with OPENAI_API_KEY", auth)
	}
	key := strings.TrimSpace(getenv("UNREAL_HARNESS_LLM_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(getenv("OPENAI_API_KEY"))
	}
	if key == "" || key == "sk-openzoo" {
		return fmt.Errorf("sign in to ChatGPT, or set OPENAI_API_KEY for API credits")
	}
	if err := setenv("UNREAL_HARNESS_LLM_PROVIDER", "openai"); err != nil {
		return err
	}
	if strings.TrimSpace(getenv("OPENAI_API_KEY")) == "" {
		return setenv("OPENAI_API_KEY", key)
	}
	return nil
}

func authFile(getenv func(string) string) string {
	if file := strings.TrimSpace(getenv("OPENAI_CODEX_AUTH_FILE")); file != "" {
		return file
	}
	home := strings.TrimSpace(getenv("CODEX_HOME"))
	if home == "" {
		home = strings.TrimSpace(getenv("HOME"))
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return ""
			}
		}
		home = filepath.Join(home, ".codex")
	}
	return filepath.Join(home, "auth.json")
}

func fileExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a file", path)
	}
	return nil
}
