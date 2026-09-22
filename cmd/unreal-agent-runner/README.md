# unreal-agent-runner

Run an AI agent from a prompt or JSON request. It writes events to stdout as
JSONL and exits when the task finishes.

Install with Go 1.27+:

```sh
go install github.com/unreallabsai/unreal-agent/cmd/unreal-agent-runner@latest
```

Set an OpenAI API key and run a prompt in the current directory:

```sh
export OPENAI_API_KEY="..."
unreal-agent-runner -p 'Inspect this project and explain how to run its tests.'
```

Or run from source at the repository root:

```sh
go run ./cmd/unreal-agent-runner -p 'Inspect this project and explain how to run its tests.'
```

Choose a workspace and save the output:

```sh
unreal-agent-runner -workspace ./my-project -p 'Summarize this project.' > run.jsonl
```

You can also pass a JSON request as an argument or through stdin:

```sh
unreal-agent-runner '{"prompt":"Summarize this project."}'
unreal-agent-runner < request.json
```

OpenAI is the default provider. Set `UNREAL_HARNESS_LLM_PROVIDER` to `openai`,
`openai-codex`, `openzoo`, `openrouter`, `fireworks`, or `ollama`, and
`UNREAL_HARNESS_LLM_MODEL` to choose a model.

### Pay per call with OpenZoo (no API key, no subscription)

[OpenZoo](https://openzoo.fun) is a local proxy that pays the zoo per request
over x402 from a burner wallet, instead of burning provider credits or a monthly
seat. Start it once, then point the runner at it:

```sh
npx openzoo                      # prints the wallet funding address, listens on :8402
UNREAL_HARNESS_LLM_PROVIDER=openzoo unreal-agent-runner -p 'Summarize this project.'
```

No key is required. The default model is `openzoo/auto`, which routes each turn
to the cheapest model that can do the job; set `UNREAL_HARNESS_LLM_MODEL` to pin
one from `GET http://127.0.0.1:8402/v1/models`. The client pins each session to a
warm upstream (`x-session-id`) and opts into one-hour prompt caching, so long
agent sessions pay for the new suffix of every turn rather than the whole prefix.
`GET /v1/info` reports spend beside what the same calls would have cost direct.

For a proxy exposed through `openzoo tunnel`, set `OPENZOO_API_KEY` (or
`UNREAL_HARNESS_LLM_API_KEY`) to the `oz_…` bearer it prints, and
`UNREAL_HARNESS_LLM_BASE_URL` to the public `/v1` URL.

Run `unreal-agent-runner -h` for options and the JSON request fields.

## Docker

The `unrea1labs/unreal-agent` image supports Linux on AMD64 and ARM64. Run it
with a project mounted as the workspace:

```sh
docker run --rm -i --user "$(id -u):$(id -g)" \
  -e OPENAI_API_KEY -v "$PWD:/workspace" \
  unrea1labs/unreal-agent:latest -p 'Summarize this project.'
```

Each release also publishes its Git tag (for example, `v0.1.0`) for version pinning.
