---
name: dispatch
description: >
  Dispatch work to another AI coding agent (Codex today) through agentcli: second
  opinion, independent diff review, cross-check, expert-persona consult, delegated
  subtask, follow-ups, background jobs. Use whenever the user asks Codex, GPT or
  OpenAI to weigh in, review or do something, even before any code is attached.
  Trigger: "pergunta pro codex", "manda pro codex", "o que o codex acha", "ask
  codex", "codex review", "get a second opinion from GPT", "act as a senior X —
  use codex", "cross-check this", "delegate to codex", "agentcli job", "follow up
  with the agent". Not for work Claude can just do (coding), in-session PR/MR
  review (cr), pre-deploy checks (ship-audit), risk analysis (premortem).
---

# Dispatch

Hand a question or a task to a second agent through `agentcli`. It is an
independent voice: different training, different blind spots, none of this
session's context. Used well it catches design flaws, invented APIs and skipped
edge cases. Used reflexively it burns minutes and tokens repeating Claude's work.

## Pick the scenario

Match the user's intent. When two fit (second opinion vs delegation), ask the
user: they differ in sandbox, read-only vs workspace-write.

Profiles ship in the binary (the codex provider's; `--scenario <name>` applies
one). Precedence: explicit flag, then profile, then provider default. A name not
in the table runs with no profile and is recorded as given.

| Scenario | Use when | Model / effort / sandbox | Detail |
|---|---|---|---|
| `second-opinion` | judge a decision, plan or design already drafted; "segunda opinião" | gpt-6.1-sol / high / read-only | `references/second-opinion.md` |
| `code-review` | independent pass on a diff: uncommitted, vs a base, one commit | gpt-6.1-sol / high / read-only | `references/code-review.md` |
| `cross-check` | sanity pass on finished code, named by files; "what did I miss" | gpt-6.1-sol / high / read-only | `references/cross-check.md` |
| `expert-persona` | judgment question framed as a senior practitioner; "como um sênior de X" | gpt-6.1-sol / xhigh / read-only | `references/expert-persona.md` |
| `delegation` | well-scoped mechanical work; the only scenario that writes | gpt-6.1-sol / medium / workspace-write | `references/delegation.md` |
| `adhoc` (default) | anything else | none | none |

Load only the reference for the chosen scenario. A user override always wins
(`--effort xhigh`, `--model <id>`, `--sandbox read-only`); say which override ran.

## Mod tools or CLI

The agent-cli mod, when loaded, gives three tools. By contract:

- `mcp__agent-cli__ask`: `prompt` required; optional `provider`, `scenario`,
  `model`, `effort`, `sandbox`, `cwd`. Always admits a job and returns
  `{conversation_id, run_id}`.
- `mcp__agent-cli__send`: `conversation_id` and `prompt` required, plus the same
  optional fields. Always admits a job.
- `mcp__agent-cli__jobs`: `action` is `list`, `status`, `result` or `cancel`;
  `run_id` is required except for `list`.

A finished job posts a notice in the session
(`agent-cli job <run_id> (<scenario>, conversation <conversation_id>) finished: <outcome>`)
with the output inline when it is at most 8 KiB, otherwise its first 8 KiB plus
the output path. Nothing to poll: launch, keep working, read the notice.

Use the mod tools for `ask` and `send` when they are present. Use the CLI when
you need what they do not carry: `review` (no mod tool), `--attr` and `annotate`,
`--timeout`, `--prompt-file`, `--json`, `--dry-run`, a foreground run, scripts,
or when the mod is not loaded. Both reach the same runs and conversations.

## Run it from the CLI

The prompt is a positional argument, `-` for stdin (use a heredoc past a couple
of lines), or `--prompt-file <path>`. Exactly one source.

**Job** (the default for every scenario: runs take minutes, and a job outlives
the call that started it):

```bash
agentcli exec --scenario second-opinion --background --json - <<'PROMPT'
...the self-contained prompt...
PROMPT
# {"conversation_id": "c-...", "run_id": "r-...", "state": "running", ...}
agentcli wait <run_id> --timeout 90 --json   # exit 5: still running, the run is untouched, wait again
agentcli result <run_id>                     # prints the provider's final message
```

Without `--json`, `--background` prints the run id as its last line. `wait`
blocks until the run is terminal and exits with the run's exit code. To be
notified instead of polling, run `agentcli wait <run_id>` through the Bash tool
with `run_in_background: true` and carry on.

**Foreground** when blocking is fine (short prompt, nothing else to do). The last
stdout line is the output path: `Read` it. Set `--timeout <seconds>`, because the
run lives and dies with the shell call holding it:

```bash
agentcli exec --scenario cross-check --timeout 110 - <<'PROMPT'
...
PROMPT
```

Review takes a target and no prompt: `agentcli review --scenario code-review
--uncommitted`, `--base <branch>` or `--commit <sha>`. It takes `--background`
the same way.

Exit codes, flags and diagnosing a failed run: `references/cli.md`.

## Continue a conversation

Every run belongs to a conversation. A follow-up sends the next turn to the same
provider session, so it keeps what was said:

```bash
agentcli send <conversation_id> --background --json "Which of those risks is cheapest to remove?"
```

`<run_id>` works in place of the conversation id. A follow-up reuses the first
turn's scenario, model, effort, sandbox and directory; flags change that turn
only. One turn at a time per conversation (a second `send` while one runs exits
3). A conversation the provider never reported a session for cannot be resumed
(exit 6). `agentcli conversations` lists them with status and turn count;
`agentcli status` lists this session's recent runs.

## Read the result

Take the output path from the mod notice, the foreground run's last stdout line,
or `wait`'s output, and `Read` it; `agentcli result <run_id>` prints the same
text. Check the outcome before trusting the text: `wait --json` and `status <run_id> --json`
report `outcome` (`error`, `empty`, `timeout`, `cancelled` or `lost` mean the run
did not deliver) and an `error_excerpt`.

A dispatch starts with zero shared context. The prompt names file paths,
pastes the snippet that matters, states the decision and Claude's current lean.
"Continue where we left off" fails; a `send` to the conversation succeeds.

## After the run

1. **Quote with attribution**, so the user hears a second voice: "Codex says…",
   in the user's language.
2. **Disagree openly** when the take conflicts with yours: "I disagree with Codex
   here because…". Compare reasoning; capitulating or rubber-stamping is
   consensus theater.
3. **Do not apply the other agent's patches** to the workspace. Review-style
   scenarios produce findings, not diffs to merge. Only `delegation` writes, and
   even then the user sees the diff before anything is committed.
4. **Record what you learn** about a run: tag it at dispatch with `--attr
   key=value`, or afterwards with `annotate`, which also works on a job that is
   still running:

```bash
agentcli annotate <run_id> --attr-json 'review.findings={"total":3,"high":1}'
```

Keys are namespaced by convention (`review.findings`, `ticket`); `--attr` stores
a string, `--attr-json` any JSON value. The mod tools carry no attributes, so
annotate after the fact.

## Cost awareness

A run takes minutes and spends reasoning tokens billed as output; effort drives
both. Raise it only when the problem is hard, not to be safe. Before a second
dispatch in a session ask whether it carries weight or is reflexive
double-checking. `agentcli stats --days 7` shows volume, reliability and
duration; `agentcli runs --days 7` emits every run as JSONL for `jq`.
