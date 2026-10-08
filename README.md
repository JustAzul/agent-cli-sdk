# agentcli

[![ci](https://github.com/JustAzul/agentcli/actions/workflows/ci.yml/badge.svg)](https://github.com/JustAzul/agentcli/actions/workflows/ci.yml)
[![dist](https://github.com/JustAzul/agentcli/actions/workflows/dist.yml/badge.svg)](https://github.com/JustAzul/agentcli/actions/workflows/dist.yml)
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
- [Cost](#cost)
- [Inside Claude Code](#inside-claude-code)
- [Using it from hooks and scripts](#using-it-from-hooks-and-scripts)
- [Retention](#retention)
- [Exit codes](#exit-codes)
- [Safety](#safety)
- [How it is built](#how-it-is-built)

## Install

```sh
claude plugin marketplace add JustAzul/agentcli
claude plugin install agentcli@agentcli
```

The plugin puts `agentcli` on the PATH of Claude's Bash tool. Its
`SessionStart` hook also keeps a small launcher at `~/.local/bin/agentcli`, so
your own settings hooks and shell scripts can call it too. The launcher only
ever points at the installed plugin. It never replaces a file it did not
write, and `AGENTCLI_NO_LINK=1` turns it off. The same hook prunes old run
directories once a day (see [Retention](#retention)).

Requirements:
- the provider CLI on `PATH` and logged in (`codex` for the Codex provider);
- Linux or macOS on amd64 or arm64.

### Update

```sh
claude plugin marketplace update agentcli
claude plugin update agentcli@agentcli
```

Restart Claude Code to load the new version. The launcher follows it at the
next session start.

### Moving from agent-cli-sdk

The project was called `agent-cli-sdk`, its plugin `agent-cli`. Remove the old
plugin and its marketplace, then install as above:

```sh
claude plugin uninstall agent-cli@agent-cli-sdk
claude plugin marketplace remove agent-cli-sdk
```

The binary, the `AGENTCLI_*` variables and the state directory keep their
names, so runs, conversations and telemetry carry over, and the launcher
points at the new plugin from the next session start. What changes is how
Claude Code names the plugin's parts: tools are `mcp__agentcli__*`, agent
types `agentcli:<scenario>`, the skill `agentcli:dispatch` and the command
`/agentcli-jobs`.

### Uninstall

```sh
claude plugin uninstall agentcli@agentcli
claude plugin marketplace remove agentcli
rm ~/.local/bin/agentcli
```

Runs and telemetry stay in `~/.local/state/agentcli` (or `$AGENTCLI_HOME`, or
`$XDG_STATE_HOME/agentcli`) until you delete it.

### Build from source

Everything builds in a pinned Go container, so the host needs only Docker (see
[How it is built](#how-it-is-built)). `make dist` writes the complete plugin,
binaries included, to `dist/`; `claude --plugin-dir dist` loads it for one
session.

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
- `progress.jsonl`, what the provider did while it worked (`agentcli progress
  <run_id>` prints it);
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
- `model_used` and `effort_used`, what the provider reports it actually ran
  with (for Codex, read from its own session file), so a run on the provider's
  default model records that model too;
- source, session id, cwd;
- exit code, outcome, duration, timeout;
- output path and size, error excerpt, token usage;
- an `attrs` object for anything else.

A model call made outside a run, such as one of the mod's progress summaries,
appends a `model_call` record instead: provider, model, source, session id,
the run it was about and its token usage. `agentcli usage add` writes it;
runs and their counts never include it.

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
agentcli stats --all --json  # usage_totals, and usage_by_model per model
agentcli stats --session-id "$CLAUDE_CODE_SESSION_ID"   # one Claude Code session's runs and tokens
```

## Cost

`stats` also reports what the runs would cost at the provider's API list
prices, in US dollars, whatever the sign-in (a subscription included). Prices
come from the public [LiteLLM price list](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json),
cached locally for the models your providers offer (`codex debug models`) and
the ones your runs used, nothing else.

```sh
agentcli prices refresh --json     # fetch the prices the cache needs (conditional, ETag)
agentcli prices refresh --max-age 24h   # only when the cache is older than a day
agentcli prices                    # what is cached, from where and when
agentcli stats --session-id "$CLAUDE_CODE_SESSION_ID" --all --json | jq .usage_totals.cost_usd
agentcli stats --session-id "$CLAUDE_CODE_SESSION_ID" --all --json | jq .usage_by_provider
```

- A run's cost counts its input in three parts: uncached input at the input
  price, cached input at the cache-read price, and tokens written to the
  prompt cache at the cache-write price. Reasoning tokens are part of output.
- Codex reports no cache writes under a ChatGPT sign-in, so when a Codex run
  reports none, every uncached input token is counted as written, as implicit
  prompt caching does. Anthropic reports the writes it made, so a model call
  is priced exactly as reported.
- `usage_totals` is what the runs cost. `usage_by_provider` splits it per
  provider and adds the model calls (`anthropic`), each with its own
  `cost_usd`.
- Costs are exact (no floating point) and rounded once: 6 decimals in JSON,
  cents in text.
- A model the price list does not price (`gpt-reserve`, `codex-auto-review`)
  makes the total a lower bound: `cost_complete` is false and the status line
  shows `≥`.
- Only short-context standard-tier prices are used: no Fast mode, no Batch, no
  long-context rate.
- `AGENTCLI_PRICES_URL` points the refresh at another copy of the list;
  `AGENTCLI_PRICES_URL=off` stops it from going to the network at all.

## Inside Claude Code

The plugin adds a `dispatch` skill and a mod.

The **skill** teaches Claude:
- when to ask another agent (second opinion, code review, cross-check, expert
  persona, delegation);
- how to run it, follow up and read the result;
- to quote the other agent's findings with attribution, and to disagree openly
  when it disagrees.

The **mod** registers one agent type per scenario, `agentcli:second-opinion`,
`:code-review`, `:cross-check`, `:expert-persona`, `:delegation` and `:adhoc`.
Claude dispatches through the Agent tool, and the run behaves like any
background agent:
- its row shows what the run is doing as a short summary every few steps
  (`» Reading workerlog.go`), with an elapsed-time line when it is quiet; with
  summaries off, the raw steps (commands it starts, text it writes);
- the native completion notification carries the output;
- a SendMessage to the agent sends the next turn of the same conversation;
- stopping the agent cancels its run.

No Claude model runs inside these agents: the mod answers each of their
requests with an agentcli job. A dispatch that fails says so in its answer
instead of falling back to a Claude reply. The types are offered to Claude only
once the dispatch skill has loaded in the session, so it does not reach for
another agent unasked. `agentcli:code-review` takes its target as the whole
prompt: `uncommitted`, `base <branch>` or `commit <sha>`.

The mod also adds three tools that start background jobs, for overrides the
agent types do not carry:

| Tool | Does |
|---|---|
| `mcp__agentcli__ask` | starts a conversation (`prompt`, plus optional `provider`, `scenario`, `model`, `effort`, `sandbox`, `cwd`) |
| `mcp__agentcli__send` | continues one (`conversation_id`, `prompt`) |
| `mcp__agentcli__jobs` | `list`, `status`, `result` or `cancel` |

When a job started through these tools finishes, the mod shows a toast and
hands Claude a notice with the outcome and the output (inline up to 8 KiB).

A run any other caller starts with `--agent-feedback` (see below), such as a
hook's review, shows in a band above the prompt while it works: one line with
its scenario, its source, how long it has run, what it has cost so far and a
short summary of what it is doing, gone once it ends. Only you see the band:
nothing about the run enters the conversation, and the caller that started it
delivers its result.

The summaries come from `claude-haiku-5-5` at low effort: the mod sends a
run's newest steps, each cut to 300 characters, to the Anthropic API through
the session's own sign-in, on the run's first step and then every three. They
are skipped when nothing is on screen (`claude -p`), and the plugin's
`summaries` option turns them off, back to the raw steps. A summary that fails
shows the raw step instead.

The status line counts the session's running jobs and shows what the
session's runs cost, hook runs included, and what the mod's own model calls
cost (`💸 1 job running · Codex $0.68 | Claude $0.01`; `≥` when part of it has
no price). Both are estimates at API list prices. The mod refreshes the price cache when the
session starts (at most once a day) and whenever the conversation is
compacted. `/agentcli-jobs` lists the session's jobs at once, even while Claude
is working.

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
- `--agent-feedback` shows the run in the band above the prompt of its Claude
  Code session while it works; `AGENTCLI_AGENT_FEEDBACK=1` does the same for a
  caller that cannot change its arguments (an older agentcli ignores the
  variable). It needs a session id; without one the run goes ahead unflagged
  and stderr says why.
- `--source` labels who dispatched the run, for `stats`.
- `--session-id` ties the run to a Claude Code session. It defaults to
  `$CLAUDE_CODE_SESSION_ID`.
- `--clean-sentinel` and `--material-label` turn "nothing to report" into the
  outcome `clean`.
- `--run-id` lets a caller pick the id.

## Retention

Run directories hold prompts and outputs, so they are pruned; telemetry is
never pruned.

```sh
agentcli prune --dry-run            # what would go
agentcli prune --older-than 14      # remove finished runs that ended over 14 days ago
```

- A run is removed only when it is finished, ended more than N days ago
  (default 30, or `AGENTCLI_RETENTION_DAYS`), is not the active turn of a
  conversation, and nothing holds its lock.
- Conversations keep working after their old runs are pruned: `send` still
  resumes them. A pruned run id reads as not found (exit 4).
- The plugin runs `agentcli prune --auto` at session start, at most once a day
  and for at most 3 seconds. `AGENTCLI_NO_PRUNE=1` turns it off.
- `status` reads a small per-session index, so its cost does not grow with the
  number of runs on disk.

## Exit codes

`agentcli` exits with the provider's own exit code, except:

| Code | Meaning |
|---|---|
| 2 | usage error (bad flag, missing prompt, reserved native flag, unsupported capability) |
| 3 | conversation busy, or `result` on an unfinished run |
| 4 | run or conversation not found |
| 5 | `wait` reached its own `--timeout` (the run keeps going) |
| 6 | conversation not resumable yet |
| 7 | `prices refresh` could not get the price list (the cache is kept) |
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
- With summaries on, the mod sends a run's newest steps, cut short, to the
  Anthropic API (see [Inside Claude Code](#inside-claude-code)); the
  `summaries` option turns that off.

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
