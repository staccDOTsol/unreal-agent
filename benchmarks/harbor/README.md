# Harbor evaluation adapter

Evaluates `unreal-agent-runner` as `unreal-agent` with Harbor 0.22.0. Requires Go
from the root `go.mod`, Python 3.12+, uv, and Docker or Modal.

## Setup and build

From the repository root:

```sh
make -C benchmarks/harbor sync
make -C benchmarks/harbor build REVISION=HEAD
```

Builds committed source into `bin/harbor/<short-commit>/` with a revision and
checksum manifest. Defaults to Linux amd64. Existing bundles are not overwritten;
set `BUNDLE` to a new directory to rebuild.

The manifest records the selected runner along with the revision.

## Run

Use an absolute bundle path:

```sh
export OPENAI_API_KEY=...
uv run --project benchmarks/harbor --locked harbor run \
  -p /absolute/path/to/task \
  -a harness_harbor.agent:UnrealAgent \
  -m openai/gpt-5.4 \
  --ak bundle="$PWD/bin/harbor/<short-commit>" \
  --ak thinking_level=high
```

`thinking_level`: `low`, `medium`, `high` (default), `xhigh`, or `max`.
Also supports `openrouter/<model>` with `OPENROUTER_API_KEY` and
`fireworks_ai/<model>` with `FIREWORKS_AI_API_KEY`. Use Harbor's `--ae` option for
environment overrides.

OpenRouter requests opt into OpenRouter's automatic prompt caching with a one-hour
TTL and carry the session id, so Anthropic and other explicit-breakpoint upstreams
cache the growing conversation, keep it on one upstream, and keep it across long
tool calls and reasoning turns.

### Pay per call through OpenZoo

`openzoo/<model>` routes the run through an [OpenZoo](https://openzoo.fun) proxy,
paying x402 per call from a burner wallet instead of a provider account. The
sandbox cannot see `localhost`, so expose the proxy and point the adapter at it:

```sh
npx openzoo tunnel            # prints https://….trycloudflare.com/v1 and an oz_… bearer
export OPENZOO_BASE_URL=https://<tunnel>/v1
export OPENZOO_API_KEY=oz_…   # in tunnel mode the key is real auth
uv run --project benchmarks/harbor --locked harbor run \
  -p /absolute/path/to/task \
  -a harness_harbor.agent:UnrealAgent \
  -m openzoo/openzoo/auto \
  --ak bundle="$PWD/bin/harbor/<short-commit>"
```

Two independent levers to A/B against a direct run of the same tasks:

1. **Rail** — `openai/<model>` vs `openzoo/<model>`: identical model, paid per call
   with the same `x-session-id` pinning and one-hour `cache_control` the
   OpenRouter client uses. The proxy's `GET /v1/info` reports `spendUsd` beside
   `directUsd` for the job.
2. **Router** — `openzoo/openzoo/auto` lets the zoo pick the cheapest model that
   passes its measured route table per turn, instead of pinning one frontier model
   for every tool call.

For Terminal-Bench 4.0 on Modal, configure Modal credentials and run:

```sh
uv run --project benchmarks/harbor --locked --extra modal harbor run \
  -d terminal-bench/terminal-bench@4.0.0 \
  -e modal -a harness_harbor.agent:UnrealAgent \
  -m openai/gpt-6-astra \
  --ak bundle="$PWD/bin/harbor/<short-commit>" --ak thinking_level=max \
  -k 5 -n 40 --job-name tb4-unreal-agent
```

Inspect results with `harbor view jobs/<job-name>`.

## Output

- Bash and ViewImage tools.
- ViewImage returns images within 2000×2000 pixels and 5 MB minus 1 KB of base64
  content. Trajectories reference the returned images under `agent/images/` and
  include original format and coordinate-scaling metadata when applicable.
- Logs and sessions are under `agent/`; `agent/trajectory.json` contains ATIF
  observations and token totals.
- Observations are grouped by tool call. Their `extra` sequence, timestamp, and
  `available_before_turn` fields preserve asynchronous timing.

## Validation

```sh
make -C benchmarks/harbor test check
make -C benchmarks/harbor smoke BUNDLE=../../bin/harbor/<short-commit>
```

The smoke test uses Docker and a local test model.

Reference: [Harbor agents](https://www.harborframework.com/docs/agents).
