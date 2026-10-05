# agent-cli-sdk

[![ci](https://github.com/JustAzul/agent-cli-sdk/actions/workflows/ci.yml/badge.svg)](https://github.com/JustAzul/agent-cli-sdk/actions/workflows/ci.yml)
[![dist](https://github.com/JustAzul/agent-cli-sdk/actions/workflows/dist.yml/badge.svg)](https://github.com/JustAzul/agent-cli-sdk/actions/workflows/dist.yml)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**`agentcli` is one command-line interface for driving agent CLIs from Claude
Code, hooks and scripts.** It runs one-shot prompts and resumable
conversations, in the foreground or as background jobs, and records every run
in an append-only telemetry stream that takes new attributes without a code
change.

Codex is the first provider. The provider interface is built so Claude Code
can follow without changing the commands, the run layout or the telemetry.

It ships as a Claude Code plugin with prebuilt static binaries for
linux/darwin × amd64/arm64. Installing it needs nothing besides the provider
CLI itself.

## Contents

- [Install](#install)
- [Quick start](#quick-start)
- [Conversations and jobs](#conversations-and-jobs)
- [Scenarios and profiles](#scenarios-and-profiles)
- [Telemetry](#telemetry)
- [Inside Claude Code](#inside-claude-code)
- [Using it from hooks and scripts](#using-it-from-hooks-and-scripts)
- [Exit codes](#exit-codes)
- [Safety](#safety)
- [How it is built](#how-it-is-built)

## Install

```sh
claude plugin marketplace add JustAzul/agent-cli-sdk
claude plugin install agent-cli@agent-cli-sdk
```

The plugin puts `agentcli` on the PATH of Claude's Bash tool. Its
`SessionStart` hook also keeps a small launcher at `~/.local/bin/agentcli`, so
your own settings hooks and shell scripts can call it too. The launcher only
ever points at the installed plugin. It never replaces a file it did not
write, and `AGENTCLI_NO_LINK=1` turns it off.

Requirements:
- the provider CLI on `PATH` and logged in (`codex` for the Codex provider);
- Linux or macOS on amd64 or arm64.

## Quick start

```sh
# A second opinion: read-only, the scenario's model and effort
agentcli exec --scenario second-opinion "Is a TTL cache safe for this lookup?"

# Review the working tree, a branch diff or one commit
agentcli review --uncommitted
agentcli review --base main
agentcli review --commit 3f2c1ab

# Long prompts: from a file or from standard input
agentcli exec --scenario cross-check --prompt-file brief.md
git diff | agentcli exec --scenario code-review -
```

A foreground run prints the path of the output file on its last stdout line.
With `--json` it prints one object instead:

```json
{"sdk_status": "ok", "exit_code": 0, "provider_exit": 0, "state": "done", "outcome": "ok",
 "run_id": "r-…", "conversation_id": "c-…", "run_dir": "…/runs/r-…", "output_path": "…/runs/r-…/output.md"}
```

`--dry-run` shows exactly what would run, without running it:

```sh
agentcli exec --scenario second-opinion --cwd /tmp/demo/repo --dry-run "Is a TTL cache safe here?"
```

```json
{
  "command": "exec",
  "provider": "codex",
  "argv": ["codex", "exec", "-C", "/tmp/demo/repo", "-s", "read-only", "-m", "gpt-6.1-sol",
           "-c", "model_reasoning_effort=high", "--json", "-o", "/tmp/demo/state/runs/r-…/output.md", "-"],
  "stdin": "prompt",
  "cwd": "/tmp/demo/repo"
}
```

## Conversations and jobs

Every run is a turn of a conversation. `exec` starts one, `send` continues it,
and both take `--background`:

```sh
# Start a job; the last stdout line is its run id
run=$(agentcli exec --scenario delegation --background --prompt-file task.md | tail -n 1)

agentcli status "$run"          # queued, running, done, failed, cancelled, timeout or lost
agentcli wait "$run" --timeout 600
agentcli result "$run"          # the output

# Follow up in the same provider session, by conversation id or by any of its run ids
agentcli send "$run" "Now add tests for the edge cases you listed."

agentcli cancel "$run"
agentcli conversations
```

How jobs behave:
- A job runs in a detached worker that outlives the Claude Code session that
  started it. There is no daemon.
- A conversation runs one turn at a time. A second turn while one is active
  exits 3.
- A worker that dies is found and marked `lost` by the next command that looks
  at its run.
- `--timeout N` stops the provider's whole process group and exits 124.

Every run keeps its files under the agentcli home: `$AGENTCLI_HOME`, else
`$XDG_STATE_HOME/agentcli`, else `~/.local/state/agentcli`. Each run directory
holds:
- `prompt.md`, `request.json` and `state.json`;
- `output.md`;
- `stderr.tail`, the last 64 KiB of provider stderr;
- for a job, `worker.log`.

## Scenarios and profiles

`--scenario` picks a built-in profile. An explicit `--model`, `--effort` or
`--sandbox` always wins, and each run records where every value came from.

| Scenario | Model | Effort | Sandbox |
|---|---|---|---|
| `second-opinion` | gpt-6.1-sol | high | read-only |
| `code-review` | gpt-6.1-sol | high | read-only |
| `cross-check` | gpt-6.1-sol | high | read-only |
| `expert-persona` | gpt-6.1-sol | xhigh | read-only |
| `delegation` | gpt-6.1-sol | medium | workspace-write |
| `adhoc` (default) | provider default | provider default | provider default |

Any other scenario name runs with no profile and is recorded as given.

## Telemetry

Each run appends one record to `telemetry/YYYY-MM.jsonl` under the home, one
file per UTC month. Files are never rewritten.

A record carries:
- run and conversation ids, turn, provider and provider version;
- command, scenario, model/effort/sandbox and their sources;
- source, session id, cwd;
- exit code, outcome, duration, timeout;
- output path and size, error excerpt, token usage;
- an `attrs` object for anything else.

Attributes are how consumers add data without a schema change:

```sh
# At dispatch time
agentcli review --base main --attr pr=128 --attr-json 'labels=["api","auth"]'

# After the fact, for example findings parsed from the output
agentcli annotate "$run" --attr-json 'review.findings={"high":1,"medium":3}'
```

Annotations are folded into their run, later values winning per key, by every
reader:

```sh
agentcli runs --days 30 | jq 'select(.attrs["review.findings"].high > 0)'
agentcli stats --days 7      # volume, outcomes, durations, reliability, findings, usage
agentcli stats --all --json
```

## Inside Claude Code

The plugin adds a `dispatch` skill and a mod.

The **skill** teaches Claude:
- when to ask another agent (second opinion, code review, cross-check, expert
  persona, delegation);
- how to run it, follow up and read the result;
- to quote the other agent's findings with attribution, and to disagree openly
  when it disagrees.

The **mod** adds three tools that always start background jobs:

| Tool | Does |
|---|---|
| `mcp__agent-cli__ask` | starts a conversation (`prompt`, plus optional `provider`, `scenario`, `model`, `effort`, `sandbox`, `cwd`) |
| `mcp__agent-cli__send` | continues one (`conversation_id`, `prompt`) |
| `mcp__agent-cli__jobs` | `list`, `status`, `result` or `cancel` |

When a job started through the mod finishes, the mod:
- shows a toast;
- hands Claude a notice with the outcome and the output (inline up to 8 KiB);
- counts running jobs in a status line.

`/agent-cli-jobs` lists the session's jobs at once, even while Claude is working.

## Using it from hooks and scripts

Settings hooks do not see the plugin's `bin/` directory, so call the launcher:

```sh
agentcli="${AGENTCLI_BIN:-$HOME/.local/bin/agentcli}"
out=$("$agentcli" exec --scenario code-review --source hook-post-commit \
        --model gpt-6.1-sol --effort high --sandbox read-only --timeout 300 \
        --clean-sentinel "No material findings." --material-label findings \
        --cwd "$repo" --prompt-file "$prompt" | tail -n 1)
```

Useful flags:
- `--source` labels who dispatched the run, for `stats`.
- `--session-id` ties the run to a Claude Code session. It defaults to
  `$CLAUDE_CODE_SESSION_ID`.
- `--clean-sentinel` and `--material-label` turn "nothing to report" into the
  outcome `clean`.
- `--run-id` lets a caller pick the id.

## Exit codes

`agentcli` exits with the provider's own exit code, except:

| Code | Meaning |
|---|---|
| 2 | usage error (bad flag, missing prompt, reserved native flag, unsupported capability) |
| 3 | conversation busy, or `result` on an unfinished run |
| 4 | run or conversation not found |
| 5 | `wait` reached its own `--timeout` (the run keeps going) |
| 6 | conversation not resumable yet |
| 70 | internal error, or a job whose worker never started |
| 124 | the run hit `--timeout` |
| 125 | the run was lost (its worker died) |
| 127 | provider CLI not found |
| 130 | cancelled by `agentcli cancel` |
| 128+n | stopped by signal n |

With `--json`, every exit path prints exactly one JSON object with
`sdk_status`, `exit_code` and `error`, so a provider's exit 2 is never confused
with a usage error.

## Safety

- `danger-full-access` is refused, whatever the scenario.
- Native flags that would override what agentcli controls are refused with exit
  2. That covers the output file, JSON mode, working directory, model, sandbox,
  config profile, `--dangerously-*`, `--ephemeral`, and `-c` overrides of
  model, effort, profile or sandbox keys. Other native flags pass through after
  `--`.
- Prompts go to the provider on stdin and are stored only in the run directory
  (mode 0600, directories 0700). Telemetry never holds the prompt.

## How it is built

- Go, standard library plus `golang.org/x/sys`, built with `CGO_ENABLED=0` and
  `-trimpath`. Everything runs in a pinned Go container, so the host needs only
  Docker:

  ```sh
  make test        # go test ./...
  make vet         # go vet ./... (and make vet-darwin)
  make build       # build/agentcli
  make dist        # the plugin tree with the four binaries and SHA256SUMS
  ```

- CI runs the tests on Linux and macOS. On every push to `main`, a separate
  workflow builds the plugin tree, scans it for personal data, and publishes it
  as a single commit on the `dist` branch. The marketplace manifest on `main`
  points at that branch.
- The plugin's mod is plain JavaScript, tested with `claude plugin test plugin`.
- The requirements and the decisions every change builds against are in
  [`docs/PRD.md`](docs/PRD.md), [`docs/test-cases.md`](docs/test-cases.md) and
  [`docs/FRAME.md`](docs/FRAME.md).

## License

MIT. See [LICENSE](LICENSE).
