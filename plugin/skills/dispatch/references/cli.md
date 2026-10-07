# CLI reference

## Exit codes

`agentcli` exits with the provider's own code, except:

| Code | Meaning |
|---|---|
| 2 | usage error caught before spawning (bad flag, no or two prompt sources, review with a prompt, reserved native flag, unsupported capability) |
| 3 | conversation busy, or `result` on a run that is not finished (the message names `wait`) |
| 4 | run or conversation not found |
| 5 | `wait` hit its own `--timeout`; the run is left running |
| 6 | conversation not resumable (no provider session id yet) |
| 70 | internal error, or a job whose worker never reached running |
| 124 | provider timed out (`--timeout`) |
| 125 | job lost (its worker died) |
| 127 | provider binary not found |
| 130 | run cancelled by request |
| 128+n | `agentcli` itself ended by signal n |

With `--json` every exit prints one JSON object (`sdk_status`, `exit_code`,
`error`, plus ids), so a provider exit 2 is distinguishable from a usage error.

Provider auth failures surface as that run's error. Tell the user to log in to
the provider themselves; do not attempt it for them.

## Flags worth knowing

Shared by `exec`, `review` and `send`:

- `--provider` (default `codex`; a conversation keeps the provider it started
  with), `--scenario`, `--model`, `--effort`, `--sandbox` (`read-only` or
  `workspace-write`), `--cwd`.
- `--timeout <seconds>`: kill the provider after this long (no default).
- `--attr key=value`, `--attr-json key=<json>` (repeatable): consumer attributes.
- `--clean-sentinel <text>` and `--material-label <label>`: outcome becomes
  `clean` when the whole output equals the sentinel (case and whitespace
  ignored), otherwise the label (default `ok`). Use them when "nothing to report"
  is a known sentence.
- `--run-id <id>`: choose the run id (letters, digits, `.`, `_`, `-`; must be new).
- `--agent-feedback` (or `AGENTCLI_AGENT_FEEDBACK=1`): show the run in the band
  above the prompt of its Claude Code session while it works: its scenario,
  source, elapsed time and newest step. It needs a session id (`--session-id`,
  else `$CLAUDE_CODE_SESSION_ID`); without one the run goes ahead unflagged and
  stderr says why. Nothing about the run is handed to Claude: whoever started
  the run delivers its result.
- `--dry-run`: print the resolved plan as JSON and execute nothing.
- `--json`: one JSON object instead of the output path.
- `-- <native flags>`: passed to the provider. `-o`, `--json`, `-C`, `-m`, `-s`,
  `-p`, `--ephemeral`, `--dangerously-*` and `-c` overrides of model, effort,
  profile or sandbox are refused (exit 2); use the agentcli flags instead.
  `--sandbox danger-full-access` is refused too.

The working-directory git check is automatic: agentcli adds the provider's
skip-git-repo-check flag when `--cwd` is not inside a git work tree, so a purely
conceptual question needs no extra flag.

## Where a run lives

`--json` output carries `run_dir`. Under it: `prompt.md` (what was sent),
`output.md` (the final message), `state.json`, `stderr.tail` (the last 64 KiB of
provider stderr; read it first when a run errors), `request.json`,
`progress.jsonl` (what the provider did while it worked), and for a job
`worker.log` (the worker's own diagnostics). The home is
`$AGENTCLI_HOME`, else `$XDG_STATE_HOME/agentcli`, else `~/.local/state/agentcli`.

## Reading history

- `agentcli status [run_id] [--json]`: one run, or this session's 20 most
  recent (jobs and foreground).
- `agentcli conversations [--json]`: provider, turns, busy/idle, last activity.
- `agentcli cancel <run_id> [--json]`: stop a queued or running run (it ends
  `cancelled`, 130); on a finished run it changes nothing and exits 0.
- `agentcli progress <run_id> [--from N] [--json]`: what the provider has done
  so far (commands it started, text it wrote); `--json` returns `next`, to pass
  back as `--from` to read only what is new.
- `agentcli runs [--days N | --all] [--session-id ID]`: folded telemetry, one
  JSON object per run, with `model_used`/`effort_used` (what the provider ran
  with) beside the requested `model`/`effort`.
- `agentcli stats [--days N | --all] [--session-id ID] [--json]`: totals,
  outcomes, durations, reliability, findings summary, tokens in total and per
  model (`usage_by_model`).
