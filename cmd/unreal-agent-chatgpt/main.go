// Command unreal-agent-chatgpt opens the official ChatGPT app.
// With a prompt, the same binary runs that prompt through the harness.
// A ChatGPT subscription login is used when ~/.codex/auth.json exists.
// Otherwise an OpenAI API key bills API credits.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/unreallabsai/unreal-agent/cmd/internal/agentrunner"
	"github.com/unreallabsai/unreal-agent/harness/tool"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Setenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, setenv func(string, string) error, stdout, stderr io.Writer) int {
	mode, rest, launch, err := classify(args)
	if err != nil {
		fmt.Fprintf(stderr, "unreal-agent++: %v\n", err)
		return 1
	}
	if len(args) == 1 && args[0] == "--pill" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := watchPill(ctx, getenv); err != nil {
			fmt.Fprintf(stderr, "unreal-agent++: %v\n", err)
			return 1
		}
		return 0
	}
	if launch {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		sys := localSystem(getenv)
		if err := ensureChatGPT(ctx, sys); err != nil {
			fmt.Fprintf(stderr, "unreal-agent++: %v\n", err)
			notifyError(err)
			return 1
		}
		startPill(sys)
		return 0
	}
	if err := configureBill(mode, getenv, setenv, fileExists); err != nil {
		fmt.Fprintf(stderr, "unreal-agent++: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return agentrunner.RunMain(
		ctx, rest, getenv, os.Environ,
		os.Stdin, stdout, stderr,
		agentrunner.Config{
			Name:         "unreal-agent++",
			ParseRequest: parseRequest,
			Providers:    agentrunner.DefaultProviders(),
		},
	)
}

func classify(args []string) (mode string, rest []string, launch bool, err error) {
	for _, arg := range args {
		switch arg {
		case "--credits":
			if mode == "sub" {
				return "", nil, false, fmt.Errorf("--sub and --credits are mutually exclusive")
			}
			mode = "credits"
		case "--sub", "--subscription":
			if mode == "credits" {
				return "", nil, false, fmt.Errorf("--sub and --credits are mutually exclusive")
			}
			mode = "sub"
		default:
			if strings.HasPrefix(arg, "-psn_") {
				continue
			}
			rest = append(rest, arg)
		}
	}
	return mode, rest, len(rest) == 0, nil
}

func parseRequest(input io.Reader) (agentrunner.Request, agentrunner.ToolFactory, error) {
	var parsed agentrunner.Request
	if err := agentrunner.DecodeRequest(input, &parsed); err != nil {
		return agentrunner.Request{}, nil, err
	}
	return parsed, func(_ context.Context, config agentrunner.ToolConfig) (agentrunner.Tools, error) {
		return agentrunner.Tools{Registry: tool.NewRegistry(config.Translators, parsed.EnabledTools(config.Names...)...)}, nil
	}, nil
}
