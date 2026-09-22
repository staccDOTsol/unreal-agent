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

#### What OpenZoo retains (read before sending private code)

The zoo is a third party between the runner and the model provider, and its
discount comes from routing and from remembering large bodies. Concretely:

- **Every prompt and completion transits the zoo's gateway, then an x402
  "door"** — a pay-per-call endpoint the zoo selects per request from its
  pool (`x402.aispace.bot`, `surplusintelligence.ai`, the x402 Bazaar). The
  door forwards to the model provider. Retention of the prompt at the door is
  the door operator's policy, not OpenAI's; the zoo adds one more party to the
  chain compared with `openai`. OpenRouter is used only as a price catalog,
  never as an upstream.
- **Large request bodies are stored server-side for up to 24 hours.** Bodies
  above `OPENZOO_CONTEXT_MIN_CHARS` (default 16384 chars) are bound into the
  zoo's leCore memory under your wallet-signed namespace and recalled on later
  calls; that is where the "big body" discount comes from. Stored text is
  compressed and integrity-signed but **not encrypted at rest**. A context is
  deleted from disk 24 h after its last use (every recall refreshes the clock),
  or immediately when you run `npx openzoo contexts --forget <hash|all>`, which
  sends a wallet-signed `DELETE /v1/contexts/:id` and only the wallet that
  bound the context can erase it. Set `OPENZOO_NO_CONTEXT_CACHE=1` to never
  bind bodies (you then pay full price for every call).
- **Small bodies are not stored** beyond the request; the gateway logs
  route/price/latency metadata per call, not prompt content.
- **Caller headers are forwarded upstream**, including `x-session-id`.
- **Payments are on public ledgers.** Each call settles as a Solana/Base
  transfer from your burner wallet, so call timing and spend are public even
  though prompt content is not.
- **Locally**, the proxy keeps `~/.openzoo/wallet.json` (your keys) and
  `~/.openzoo/contexts.json` (corpus hashes → context ids), both chmod 600.

Roadmap, not shipped: client-side encryption of spilled bodies with a
wallet-derived key (the gateway would still see plaintext in flight to the
door, so the honesty gain over the TTL + forget above is small).

Treat `openzoo` like any other hosted inference vendor with memory enabled:
fine for benchmarks and public repos, think before sending code you cannot
hand to a third party.

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
