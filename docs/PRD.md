# Product Requirements Document

**Project / Feature**: agentcli (`agentcli`)
**Date**: 2026-10-04 (updated 2026-10-08: session cost, group Q)

---

## 1. Executive Summary

### Problem Statement
Claude Code reaches other agent CLIs (today only Codex) through three unrelated paths: a wrapper script used by a skill, three autonomous review hooks that call the provider directly, and a parallel-review pattern that launches the provider under `nohup` with no telemetry. These paths write two incompatible telemetry schemas, cannot continue a conversation, have no managed background execution, and must each be rewritten to add another provider.

### Proposed Solution
A single static Go binary, `agentcli`, that is the only way any consumer talks to an agent CLI. It offers one provider-agnostic command surface for one-shot runs and resumable conversations, foreground or as background jobs, and one extensible, append-only telemetry stream. It ships as a Claude Code plugin with zero runtime dependencies: a skill, a thin mod that gives Claude native tools and completion notices, and a launcher for shell callers. All existing consumers are migrated to it, and the old wrapper is removed.

### Business Impact
- One dispatch path: adding Claude Code as the next provider changes one adapter, not every consumer.
- Every dispatch becomes observable: token usage, outcome, error reason and arbitrary consumer attributes in one stream that never loses history.
- Claude Code gains agent-to-agent conversations it can continue later and background jobs that announce their own completion.

### Success Metrics
- **Direct provider call sites left in migrated consumers:** 0. Today there are 5 executable call sites (Observed by text search: three hook cores, the wrapper script, and the parallel-review reference). Verified by the same text search after migration (FR56).
- **Runs with a telemetry record:** 100% of `agentcli` runs, including the parallel-review path, which records 0 today (Observed: its dispatch has no telemetry step).
- **Error runs carrying an `error_excerpt`:** ≥ 90%. Baseline Observed (30-day window ending 2026-10-05): 327 of 335 exit-1 errors from the reflection hook carry none. *Assumption: 90% target — needs validation against the first 30 days of post-migration telemetry.*
- **`stats` parity on imported history:** 100% equality, against the retired analyzer over the same legacy file, for every key the retired analyzer emits.
- **Wrapper overhead:** ≤ 50 ms p50 added wall time for a foreground run against a no-op fake provider. *Assumption: needs validation by benchmark. Measured interpreter startups for comparison: bash+jq ≈ 10 ms, python ≈ 25 ms, node ≈ 21 ms.*
- **Runtime dependencies for plugin users:** 0 beyond the provider CLI itself. Verified in a clean container that has no Go, Python, Node or jq.
- **Session cost exactness:** 0 difference, at 6 decimal places, between the `cost_usd` that `stats` reports and an independent exact recomputation under FR74 from the same run usage and cached prices. Verified on fixed fixtures in the test suite and once on the owner's telemetry.

---

## 2. Problem Definition

### Target Users
- **Primary:** the owner's Claude Code sessions. Skills and Claude itself ask other agents for second opinions, reviews, cross-checks, expert personas and delegated work.
- **Secondary:** the owner's autonomous automation: the post-commit review hook, the end-of-turn dirty-tree review hook, the end-of-turn reflection hook, the parallel-review step of the code-review skill, and its eval harness.
- **Tertiary:** public plugin users who install the plugin from GitHub and want the same capability.

### Problem Statement
Observed in the owner's telemetry (1,000 records, 2026-09-11 to 2026-10-05): 961 records come from hooks that bypass the wrapper and 39 from the wrapper itself. The two writers share one file but disagree on schema: 9 fields vs 12, and only the hooks record outcome, findings and error reason. A third path, the parallel review, writes nothing. The file is truncated to its last 1,000 lines on every write, which keeps about three weeks. Model pins have been moved by hand across several files at least four times (Observed in version history). Nothing supports continuing an earlier exchange or tracking work left running in the background.

### User Pain Points
- Adding a second agent CLI means rewriting every consumer.
- Telemetry cannot answer "what did this cost" (no token counts) or "why did it fail" (most errors carry no reason), and it cannot take new attributes without editing every writer.
- History older than about three weeks is gone.
- Claude can only fire one-shot prompts. It cannot hold a conversation with another agent or be told when background work finishes.
- The plugin `bin/` directory reaches Claude's Bash tool but not settings hooks (Observed), so shell automation cannot rely on it.
- A token count says little about what a session would cost: most input is read from the prompt cache at a tenth of the input price or less (Observed: 84.7% of all recorded input tokens on 2026-10-08), so the same count can mean very different amounts.

### Business Case
Cross-model review is a recurring part of the owner's workflow: about 42 dispatches per day (Inferred: 1,000 observed records over 24 days). A single, observable, extensible dispatch layer turns that from three fragile scripts into one maintained tool, makes cost and reliability measurable, and opens the path to more providers without touching consumers.

---

## 3. Solution Scope

### In Scope
- The `agentcli` binary: CLI surface, provider interface, Codex adapter, conversations, jobs, run artifacts, telemetry, profiles, launcher.
- Distribution as a Claude Code plugin from a new public repository: marketplace manifest on the default branch, prebuilt plugin on a CI-built `dist` branch.
- A thin mod: one agent type per scenario that dispatches show as native background agents, native tools for Claude, job-completion notices, a status line with running jobs and the session's cost in US dollars, a `/agentcli-jobs` command, and a band above the prompt that shows the runs other callers flag.
- A local price cache, refreshed from a public price list and holding only the models the providers offer or have run, from which `stats` and the status line compute cost (group Q).
- A provider-agnostic dispatch skill (a rewrite of the current Codex skill) with evals.
- Migration of every live consumer in the owner's private configuration, import of the legacy telemetry, and removal of the old wrapper and skill.
- Installation on the owner's machine and end-to-end validation (real per-scenario smokes, a real post-commit hook review).

### Out of Scope
- Implementing adapters for other providers (Claude Code, Gemini). The interface must accommodate Claude Code, but no Claude adapter ships in v1.
- Summarizing conversations.
- Moving the owner's review and reflection hooks into the plugin. They stay in the owner's configuration and call `agentcli`.
- An importable Go API. The v1 public contract is the CLI, its documented JSON outputs and the mod's tools; Go packages are internal.
- Windows.
- A user-editable profile override file.
- Concurrency caps, queues or a resident daemon.
- The dormant proof-of-concept that references the old wrapper path.
- Fixing why the reflection hook fails so often. v1 only makes the failure reason visible.
- Subscription credits. Session cost is the API list-price equivalent in US dollars, whatever the provider's sign-in method.
- Long-context, Fast, Flex and Batch pricing. Cost uses the standard tier's short-context prices.

### MVP Definition
Every P0 row in FR groups A–N. The mod (group K) is part of v1 but is built after the CLI core (groups A–H) is complete and verified. P1 rows (FR10, FR17, FR36) are wanted but do not gate v1. v1 is done when every P0 FR is met, the per-scenario smokes and the post-commit E2E pass, the old wrapper is deleted, and both repositories are pushed.

---

## 4. User Stories & Requirements

### User Stories

```
US1 — Second opinion from Claude
As a Claude Code session
I want to ask another agent a question through one tool call
So that I get an independent answer without composing provider-specific flags

Acceptance Criteria:
- [ ] Claude calls the mod tool `ask` with a prompt and optional scenario; it returns a conversation id and a job id immediately
- [ ] When the job finishes, a notice starts a new turn containing the outcome and the output (inline up to 8 KiB, otherwise the first 8 KiB plus the output path)
- [ ] The run appears in telemetry with source `mod`, the scenario, token usage and outcome
```

```
US2 — Continue the conversation
As a Claude Code session
I want to send a follow-up to an earlier answer
So that the other agent keeps the context of the first exchange

Acceptance Criteria:
- [ ] `send <conversation_id> "<follow-up>"` (or the mod tool `send`) resumes the same provider session
- [ ] The follow-up is recorded as turn 2 of the same conversation
- [ ] A follow-up sent while a turn of that conversation is still queued or running is refused with exit 3 and a message naming the active job
```

```
US3 — Autonomous review hook
As the post-commit review hook
I want to run a provider review in the foreground with a hard timeout and a pinned model
So that I keep my current behaviour while gaining unified telemetry

Acceptance Criteria:
- [ ] The hook runs `agentcli exec` with explicit model, effort, timeout 300, source, clean sentinel and material label, and reads the output path from the last stdout line
- [ ] A provider that exceeds the timeout returns exit 124 within the timeout plus 15 seconds, with no surviving descendant processes
- [ ] The hook attaches parsed findings with `annotate`, and `stats` reports them in the findings summary
```

```
US4 — Parallel review that a later shell harvests
As the code-review skill
I want to start a provider review in the background and collect it later
So that both reviewers run concurrently without a hand-rolled nohup pattern

Acceptance Criteria:
- [ ] `review --base <branch> --background --json` returns the job id and paths at once
- [ ] `wait <job_id>` from another shell returns the provider's exit code and prints the output path as its last line
- [ ] The job writes a telemetry record like any other run
```

```
US5 — Install with nothing else to install
As a public plugin user
I want to install the plugin from GitHub and have `agentcli` work
So that I don't need any language runtime besides the provider CLI

Acceptance Criteria:
- [ ] `claude plugin marketplace add <owner>/agentcli` plus `claude plugin install agentcli@agentcli` installs a working binary for linux/darwin × amd64/arm64
- [ ] `agentcli` resolves in Claude's Bash tool, and after the first session start also in settings hooks via the launcher
- [ ] No Go, Python, Node or jq is required
```

```
US6 — Telemetry that grows with new questions
As the owner
I want to attach new attributes to runs, at dispatch time or later, without changing the tool
So that I can answer questions I can't predict today

Acceptance Criteria:
- [ ] `--attr key=value` and `--attr-json key=<json>` at dispatch time land in the run record's `attrs`
- [ ] `annotate <run_id> --attr key=value` after completion is folded into the run by every reader
- [ ] `runs --json` exports folded runs that `jq` can query with no code change
```

```
US7 — One source of truth for numbers
As the owner
I want the existing stats report to keep working over the full history
So that nothing I read today breaks after the migration

Acceptance Criteria:
- [ ] The legacy telemetry is imported once and re-running the import adds nothing
- [ ] `stats --all --json` over the imported history equals the retired analyzer's output for every key that analyzer emits
- [ ] The stats slash command runs `agentcli stats`
```

```
US8 — Session cost at a glance
As the owner, working in a Claude Code session
I want the status line to show what the session's agent runs would cost at API list prices
So that I know the cost even on a subscription, without reading token counts

Acceptance Criteria:
- [ ] With prices cached, the status line shows `Codex $X.XX` for every run of the session, hook runs included
- [ ] When part of the session's usage has no price it shows `Codex ≥$X.XX`; when none of it has, it shows nothing for Codex
- [ ] Compacting the main conversation refreshes the price cache without delaying the compaction
- [ ] `agentcli stats --session-id <id> --all --json` reports the same cost, exact to 6 decimal places
```

### Functional Requirements

#### A. CLI surface and exit contract

| ID | Requirement | Priority |
|----|-------------|----------|
| FR1 | `agentcli exec` starts a new conversation and runs its first turn. The prompt comes from a positional argument, from standard input when the argument is `-`, or from `--prompt-file`. Exactly one prompt source is required. | P0 |
| FR2 | `agentcli review` runs a provider review against exactly one target: `--base <branch>`, `--uncommitted` or `--commit <sha>`. It accepts no prompt. A prompt with a target is refused before spawning, with exit 2. | P0 |
| FR3 | `agentcli send <conversation_id or run_id>` runs the next turn of the referenced conversation. A run id resolves to its conversation. Prompt sources are as in FR1. | P0 |
| FR4 | Common flags on exec/review/send: `--provider` (default `codex`), `--scenario` (default `adhoc`), `--effort`, `--model`, `--sandbox` (`read-only` or `workspace-write`), `--cwd`, `--source` (default `cli`), `--session-id` (default the `CLAUDE_CODE_SESSION_ID` environment value, else empty), `--timeout <seconds>` (default none), `--run-id`, `--clean-sentinel`, `--material-label` (default `ok`), `--attr key=value` (repeatable), `--attr-json key=<json>` (repeatable), `--background`, `--json`, and `--` followed by native provider flags. | P0 |
| FR5 | Foreground contract. **Strategy:** without `--json`, stdout carries exactly one line, the absolute path of the run's output file (so it is also the last line). Warnings and progress go to stderr. The process exit code is the provider's exit code, except for these SDK codes: 2 usage error detected before spawning; 3 conversation busy; 4 run or conversation not found; 6 conversation not resumable; 7 price list unavailable (FR72); 70 internal SDK error; 124 provider timeout; 125 job lost; 127 provider binary not found; 130 run cancelled by request (FR24); 128+n when `agentcli` itself is terminated by signal n. With `--json`, stdout is one JSON object with `conversation_id`, `run_id`, `state`, `outcome`, `sdk_status`, `provider_exit`, `exit_code`, `output_path` and `run_dir`, so a provider exit 2 is distinguishable from an SDK usage error. | P0 |
| FR6 | `--background` admits a job and returns. **Strategy:** exit 0 once the worker is confirmed running. The last stdout line is the job id. With `--json`, stdout carries `{conversation_id, run_id, state, run_dir, output_path}`. | P0 |
| FR7 | `agentcli status [run_id] [--session-id <id>] [--json]` prints one run's state, or with no id the active and recent runs of the given session id (default as in FR4; most recent 20, foreground and jobs). `agentcli wait <run_id> [--timeout <seconds>] [--json]` blocks until the run is terminal, then prints like FR5 for that run and exits with the run's recorded exit code. If its own timeout expires first, it exits 5 and leaves the run running. `agentcli result <run_id>` prints the output file's content. `agentcli cancel <run_id>` requests cancellation (FR24). `agentcli conversations [--json]` lists conversations with provider, turn count, status and last activity. | P0 |
| FR8 | `--dry-run` on exec/review/send resolves profile, flags, conversation and provider plan, prints the plan (argv, stdin source, cwd, environment additions) as JSON, and executes nothing. No run directory, conversation or telemetry is created. | P0 |
| FR9 | `--run-id` is caller-supplied, must match `[A-Za-z0-9._-]{1,128}`, must not be `.` or `..`, must not start with `-` or `.`, and must not already exist (exit 2 otherwise). Generated ids are `r-<UTC yyyymmddThhmmssZ>-<8 lowercase hex>`. Conversation ids are `c-<same shape>`. | P0 |
| FR10 | `agentcli version [--json]` prints the version (from the repository's version file), source commit, `build_seq` and target platform. | P1 |

#### B. Provider interface and Codex adapter

| ID | Requirement | Priority |
|----|-------------|----------|
| FR11 | Each provider declares its capabilities: supported commands (exec, review, resume), review targets, sandbox modes, whether it reports usage, and whether it pre-assigns session ids. A request using an unsupported capability is refused before spawning, with exit 2 and a message naming the capability. | P0 |
| FR12 | Each provider builds a plan from a request: argv, stdin source, working directory, and environment additions/removals. **Strategy:** the provider process inherits the caller's environment by default, then the plan's additions and removals apply. An inherited value the caller sets for the provider reaches it unchanged. | P0 |
| FR13 | Codex plan. **Strategy:** every command runs as `codex exec`. Parent flags come first: `-C <cwd>`; `-s <sandbox>` when set; `-m <model>` when set; `-c model_reasoning_effort=<effort>` when set; `--json`; `-o <output file>`; then native passthrough flags. Then exactly one tail: `-` for exec; `review <target flag>` for review; `resume <provider session id> -` for a follow-up. The prompt is always written to the provider's standard input through a pipe. Review gets an empty standard input. `--skip-git-repo-check` is added when the working directory is not inside a git work tree. `--ephemeral` is never added. | P0 |
| FR14 | Reserved native flags are refused with exit 2 when passed after `--`: `-o`/`--output-last-message`, `--json`, `-C`/`--cd`, `-m`/`--model`, `-s`/`--sandbox`, `-p`/`--profile`, any `--dangerously-*` flag, `--approve-for-me`, `--ephemeral`, and `-c` overrides of `model`, `model_reasoning_effort`, `profile` or any `sandbox*` key (a provider config profile can set the model and the sandbox). `--sandbox danger-full-access` is refused. | P0 |
| FR15 | Codex event parsing. **Strategy:** read the JSONL event stream line by line as it arrives. On `thread.started` take `thread_id` as the provider session id and persist it immediately (FR18). On `turn.completed` take `usage` (input, cached input, cache-write input, output, reasoning output tokens); a review whose usage is all zeros records usage as null. On `error` or `turn.failed` keep the last message as the error excerpt candidate. Unknown event types are ignored. Unparseable lines are counted, not fatal, and the count is stored in the run's state file as `unparsed_events`. | P0 |
| FR16 | The error excerpt is the last error event message, or, when none exists and the exit is non-zero, the last non-empty line of the provider's stderr. It is stripped of ANSI escapes and truncated to 200 characters. | P0 |
| FR17 | `provider_version` is captured for every run by invoking the provider's version flag. A failure to capture leaves it null and never fails the run. | P1 |

#### C. Conversations

| ID | Requirement | Priority |
|----|-------------|----------|
| FR18 | Every run belongs to a conversation. A conversation record holds its provider (immutable), provider session id, working directory, the defaults of its first turn (scenario, model, effort, sandbox), its ordered list of turn run ids, its active-turn marker, and its status: `busy` while the marker is set, otherwise `idle`; plus `resumable` (true once a provider session id exists). **Strategy:** the provider session id is written to the conversation record as soon as FR15 sees it, before the run finishes. | P0 |
| FR19 | Conversation reservation. **Strategy:** admitting a turn takes an exclusive lock on the conversation, checks that no turn is queued or running, allocates the next turn number, records the new run as the active turn, and releases the lock. The active-turn marker is cleared when that run reaches a terminal state (FR22 ordering). A competing admission sees the marker and exits 3. A marker whose run is `lost` (FR23) is cleared by the reconciliation that detects the loss. | P0 |
| FR20 | Follow-ups run in the conversation's working directory with its first-turn defaults, unless the follow-up passes flags. Per-turn flags apply to that turn only and do not change the defaults. A conversation with no provider session id is not resumable: `send` exits 6 and names the reason. A conversation whose working directory no longer exists is refused by `send` with exit 2, unless `--cwd` is passed for that turn. A provider rejecting the session surfaces as that turn's provider error. A `send` passing a `--provider` different from the conversation's provider exits 2. | P0 |

#### D. Jobs (background runs)

| ID | Requirement | Priority |
|----|-------------|----------|
| FR21 | Admission and handoff. **Strategy:** with `--background`, the parent fully reads the prompt (including standard input), writes the prompt and the resolved request into the run directory, records state `queued`, starts the worker as a new process session (detached from the caller's terminal and process group), and waits until the state file shows `running` (the worker writes it only after taking the run lock), polling for at most 10 seconds. Only then does it exit 0 (FR6). If the worker fails to start, or does not reach `running` within 10 seconds, the parent kills it, records state `failed` with an error excerpt, clears the conversation marker, appends the run's telemetry record (outcome `error`), and exits 70. | P0 |
| FR22 | State machine. **Strategy:** the state file is the single authoritative record. States are `queued → running → done or failed or cancelled or timeout`, plus `lost`. The worker owns every transition after `queued`. The state also records the worker's pid and, once spawned, the provider's process group id and process start time. Each terminal transition records exit code, outcome and timestamps, and is written only after the output file is final. Order: output file final → state file terminal (atomic replace) → conversation active-turn marker cleared → telemetry record appended. | P0 |
| FR23 | Liveness and loss. **Strategy:** the worker holds an exclusive advisory lock on the run's lock file for its whole life, through a descriptor the provider does not inherit. Any command that reads a non-terminal run (status, wait, cancel, result, send) checks the lock without blocking. The run is lost when the lock is free and either the state is `running`, or the state is `queued` and more than 30 seconds have passed since admission (beyond the 10-second admission window, so an admission in progress is never misread). That command then marks the run `lost` (exit code 125), kills the recorded provider process group only when its leader still exists with the recorded start time (otherwise it kills nothing), clears the conversation marker, and appends the telemetry record. | P0 |
| FR24 | Cancellation. **Strategy:** `cancel` writes a durable cancel request into the run directory. If the run is still `queued` and the worker has not taken the lock, `cancel` marks it `cancelled` itself; a worker that then starts finds the terminal state or the cancel request, exits without spawning the provider, and leaves the state untouched. Otherwise it sends SIGTERM to the worker. The worker terminates the provider's process group: SIGTERM, a 5-second grace period, then SIGKILL. The worker then finalizes as `cancelled` (exit 130). A cancel request that arrives after the provider has already exited has no effect: the worker finalizes with the provider's result. A `cancel` on a terminal run is a no-op and exits 0 with its terminal state. | P0 |
| FR25 | Jobs survive the end of the Claude Code session that started them. There is no daemon and no concurrency limit. | P0 |

#### E. Foreground execution

| ID | Requirement | Priority |
|----|-------------|----------|
| FR26 | A foreground run holds the run lock and maintains its state file exactly like a job, so `status` and `wait` from another shell see it. The provider runs in its own process group. **Strategy:** on `--timeout` expiry, terminate the whole group (SIGTERM, 5-second grace, SIGKILL), drain the pipes for at most 5 seconds, record outcome `timeout`, and exit 124. When `agentcli` receives SIGINT/SIGTERM in the foreground, it forwards termination to the provider group the same way and records `cancelled`. It exits 130 if a cancel request (FR24) exists for the run, otherwise 128+n. With `--timeout 300` the command returns within 300 seconds plus 15 seconds of shutdown margin. | P0 |

#### F. Run artifacts

| ID | Requirement | Priority |
|----|-------------|----------|
| FR27 | Home directory: `AGENTCLI_HOME`, else `$XDG_STATE_HOME/agentcli`, else `~/.local/state/agentcli`. Each run owns `runs/<run_id>/` containing `prompt.md` (the full prompt), `request.json` (the resolved request and plan), `output.md` (the provider's final message), `state.json`, `stderr.tail` (the last 64 KiB of provider stderr) and a lock file. Conversations live in `conversations/<conversation_id>.json`. All paths are fixed when the run is admitted. | P0 |
| FR28 | Outcome classification: `error` when the exit is non-zero and the run is not timeout/cancelled/lost; `timeout`; `cancelled`; `lost`; `empty` when the exit is 0 and the output file is missing or empty; `clean` when the whole output, whitespace-stripped and lowercased, equals the `--clean-sentinel` likewise normalized; otherwise the `--material-label` value. | P0 |
| FR29 | A failure to write the telemetry record or `stderr.tail` never changes the exit code; it prints one warning line on stderr. A failure to write the terminal state file also leaves the exit code unchanged and prints a warning; other readers then reconcile that run as `lost` (FR23). | P0 |

#### G. Telemetry v1

| ID | Requirement | Priority |
|----|-------------|----------|
| FR30 | Telemetry files are `telemetry/<YYYY-MM>.jsonl` under the home directory, one per UTC month, append-only, never truncated or rewritten. A record goes to the file of the UTC month in which it is appended (imported records excepted, FR37). **Strategy:** each record is written with a single write call while holding an exclusive lock on the telemetry lock file, waiting at most 5 seconds for the lock (after which the record is skipped with an FR29 warning). Before appending, if the file does not end in a newline, a newline is written first. | P0 |
| FR31 | Run record fields: `v` (1), `kind` (`run`), `run_id`, `ts` (UTC start, RFC 3339), `provider`, `provider_version`, `command` (`exec`, `review` or `send`), `scenario`, `model`, `model_source`, `effort`, `effort_source`, `sandbox`, `source`, `session_id`, `conversation_id`, `turn`, `cwd`, `background` (bool), `exit_code`, `outcome`, `duration_ms`, `timeout_s`, `output_file`, `output_bytes`, `error_excerpt`, `usage` (object or null), `provider_session_id`, `attrs` (object). `model_source` and `effort_source` are `flag`, `profile` or `provider-default`. | P0 |
| FR32 | `agentcli annotate <run_id or output path> --attr key=value --attr-json key=<json>` appends an annotation record `{v, kind: "annotation", run_id, ts, attrs}`. An unknown run id exits 4. | P0 |
| FR33 | Fold rules for every reader. **Strategy:** read month files in ascending order and lines in file order; that append order is the replay order. A windowed read covers every month file from the window start's month through the current month (a run's record is always appended at or after its `ts`). A run's `attrs` start from its run record. Each later annotation for the same run shallow-merges its attrs into it, one top-level key at a time, later value wins, and `null` is stored as a value (not a deletion). Annotations are folded before any date filter; the filter applies to the run's `ts`. An annotation that precedes its run record in append order (a job annotated while running) is applied once that record is read; one whose run never appears creates nothing. Records with an unknown `v` or `kind`, and unparseable lines, are skipped and counted: `runs` reports the counts in one stderr line, `stats --json` in a `skipped` object (`unknown_kind`, `unknown_version`, `unparseable`). | P0 |
| FR34 | `agentcli runs [--days N or --all] [--json]` emits folded runs as JSONL (default window 7 days), one object per run, for ad-hoc analysis with `jq`. | P0 |
| FR35 | `agentcli stats [--days N or --all] [--json]` (default window 7 days) emits the same top-level keys and value semantics as the retired analyzer: `total`, `empty`, `span`, `by_source`, `by_source_status`, `instrumentation`, `reliability`, `outcomes`, `duration_ms`, `findings`, `review_findings_proxy`, `window_days`. **Strategy:** a run's legacy-style `status` is derived from its outcome (`error`, `timeout`, `cancelled`, `lost` → `error`; `empty` → `empty-output`; others → `ok`), except imported records, which keep their original `status`. A null outcome (imported records that predate it) counts as `no-outcome`. The findings summary reads `attrs["review.findings"]`, or `findings` on imported records. It adds the keys `by_provider`, `usage_totals`, `usage_by_model` (FR70), `skipped` (FR33), `unpriced_models`, `missing_prices` and `prices_checked_at` (FR75), and no other. | P0 |
| FR36 | Consumer attribute keys are namespaced by convention, e.g. `review.findings`. A key is any string. The value is any JSON value. | P1 |
| FR37 | Legacy import (a maintenance script in the repository, not a CLI subcommand). **Strategy:** read the legacy file line by line. Each line becomes a run record with `v: 1`, `kind: "run"`, `provider: "codex"`, `run_id: "legacy-" + first 16 hex of sha256(raw line + "\n" + zero-based occurrence index of that exact line within the file)`, the legacy fields mapped by name (fields the legacy record lacks, such as `command`, are null), `findings` moved to `attrs["review.findings"]`, `attrs.legacy: true`, and the record placed into the month file of its `ts`. A record whose `run_id` already exists is skipped, which makes the import idempotent. The import holds the telemetry lock for its duration. | P0 |

#### H. Profiles

| ID | Requirement | Priority |
|----|-------------|----------|
| FR38 | Built-in profiles per scenario, shipped in the binary. Precedence: explicit flag, then profile, then provider default. **Strategy:** defaults for the codex provider: `second-opinion` model gpt-6.1-sol, effort high, sandbox read-only; `code-review` model gpt-6.1-sol, effort high, sandbox read-only; `cross-check` model gpt-6.1-sol, effort high, sandbox read-only; `expert-persona` model gpt-6.1-sol, effort xhigh, sandbox read-only; `delegation` model gpt-6.1-sol, effort medium, sandbox workspace-write; `adhoc` none. A scenario name not in the table runs with no profile and is recorded as given. | P0 |

#### I. Distribution and launcher

| ID | Requirement | Priority |
|----|-------------|----------|
| FR39 | The repository's default branch holds the source and a marketplace manifest whose single plugin, `agentcli`, has source `{github, repo: <owner>/agentcli, ref: dist}` with no pinned sha. | P0 |
| FR40 | CI builds the `dist` branch on every push to the default branch. **Strategy:** a serialized workflow (one concurrency group; a newer run cancels an older one) builds static binaries for linux/darwin × amd64/arm64 with cgo disabled and path trimming, embedding source commit and `build_seq` (the default branch's commit count, computed from a full-history checkout). Before publishing, it verifies that the built commit is still the default branch's HEAD; otherwise it exits without publishing. It force-pushes a single orphan commit to `dist` containing the plugin manifest (no version field), `bin/agentcli`, the four binaries, `SHA256SUMS`, the skill, the mod, commands and hooks. A local `make dist` reproduces the same tree inside Docker. | P0 |
| FR41 | `bin/agentcli` in the plugin is a POSIX shell shim that maps `uname -s` (`Linux` → linux, `Darwin` → darwin) and `uname -m` (`x86_64`/`amd64` → amd64, `aarch64`/`arm64` → arm64) to one of the four binaries and executes it with all arguments. An unsupported platform prints the supported list and exits 70. | P0 |
| FR42 | The plugin declares a SessionStart hook that runs `agentcli link --quiet`. | P0 |
| FR43 | `agentcli link [--quiet]`. **Strategy:** resolve this plugin's authoritative install path from the Claude Code installed-plugins record. Under an exclusive lock, write the launcher (default `~/.local/bin/agentcli`) via temp file plus rename, as a POSIX shell script that executes that install's `bin/agentcli` and records its `build_seq`. It writes only when: the launcher is absent; or the existing launcher is an agentcli launcher with a lower `build_seq`; or it names an install path that no longer exists. It never overwrites a file that is not an agentcli launcher (for example a pipx-installed entry point), and says so unless quiet. `AGENTCLI_NO_LINK=1` makes it a no-op. A missing `~/.local/bin` is created. The install record used is the one for `agentcli@agentcli`, preferring user scope. When no record exists (for example a development build), `link` exits 4 with a message unless `--target <path to an agentcli shim or binary>` is given, in which case that path is used with the running binary's `build_seq`. A launcher whose target no longer exists prints that the agentcli plugin is not installed and exits 127. | P0 |

#### J. Public-repository privacy

| ID | Requirement | Priority |
|----|-------------|----------|
| FR44 | No tracked file and no `dist` artifact contains a personal email, a home-directory path, or a real provider session/thread id. Recorded provider event fixtures are sanitized with fixed fictitious ids and neutral paths. **Strategy:** a CI test scans tracked files and the built dist tree. It fails on any email not ending in `@users.noreply.github.com` or `@example.com`/`@example.org`, on any `/home/<name>` or `/Users/<name>` path, and on any UUID-shaped id not in the fixtures' allowlist of fictitious ids. Binaries are scanned through their printable strings. | P0 |
| FR45 | Commits use the owner's global git identity. LICENSE is MIT with the owner's name. | P0 |

#### K. Mod (built after the CLI core)

| ID | Requirement | Priority |
|----|-------------|----------|
| FR46 | The mod registers three tools, all executing the plugin's own `bin/agentcli`. `ask` takes `prompt` (required), `provider`, `scenario`, `model`, `effort`, `sandbox`, `cwd`; it always admits a job and returns `{conversation_id, run_id}`. `send` takes `conversation_id` and `prompt` (required) plus the same optional fields, and always admits a job. `jobs` takes `action` (`list`, `status`, `result`, `cancel`) and `run_id` (required except for `list`). Each call passes `--source mod` and the session id. | P0 |
| FR47 | Completion notices. **Strategy:** a timer every 15 seconds lists this session's recent jobs (`status --json` with the session id) and considers those started by the mod (recorded `source` `mod`; a job started from the CLI belongs to its starter), skipping the tick if the previous one is still running. For each job that is terminal and whose id is not in the mod's persistent "notified" set, it adds the id to the set, shows a toast, and submits a notice: `agentcli job <run_id> (<scenario>, conversation <conversation_id>) finished: <outcome>`, followed by the output inline if it is at most 8 KiB, otherwise its first 8 KiB plus the output path. The set survives mod reloads and keeps the 500 most recent ids (older ids are pruned; their jobs are older than any session's status window). | P0 |
| FR48 | A status line shows the count of this session's running jobs started by the mod while it is non-zero. `/agentcli-jobs` prints the session's recent jobs immediately, even while Claude is working; the command is not `/agentcli:jobs` because a mod command cannot carry the plugin namespace. | P0 |
| FR49 | Before implementing the mod, confirm the mods API types on the installed Claude Code build. A mismatch with FR46–FR48 is a blocker, reported, not worked around. | P0 |

#### L. Dispatch skill

| ID | Requirement | Priority |
|----|-------------|----------|
| FR50 | The plugin ships a provider-agnostic skill, `dispatch`, replacing the current Codex skill. It covers: the scenarios and their profiles; when to use the mod tools vs the CLI; foreground vs job; continuing conversations; reading results; quoting another agent's findings with attribution and disagreeing openly; never applying another agent's patches unless the scenario is delegation; and cost awareness. It ships trigger evals carried over from the current skill's eval set with renamed targets. | P0 |

#### M. Consumer migration (owner's private configuration)

| ID | Requirement | Priority |
|----|-------------|----------|
| FR51 | Cutover order. **Strategy:** (1) dist published and plugin installed from the GitHub marketplace, launcher present; (2) dispatch skill live and the old skill's references renamed; (3) the three review/reflection hooks migrated; (4) parallel review migrated; (5) analyzer, its slash command and the eval harness migrated; (6) legacy telemetry imported and the old file archived (renamed, no longer written); (7) old wrapper and old skill directory deleted. Each step is verified before the next starts. | P0 |
| FR52 | The post-commit review, dirty-tree review and reflection hooks call `${AGENTCLI_BIN:-$HOME/.local/bin/agentcli} exec` in the foreground with explicit `--model` (gpt-6.1-sol, gpt-6.1-sol, gpt-6-luna) and `--effort` (high for all three), `--sandbox read-only`, `--timeout` (300, 300 and 240), `--source` (`hook-post-commit`, `hook-stop`, `hook-stop-claude-md`), `--scenario` (`code-review`, `code-review`, `claude-md-update`), `--cwd <repo root>`, `--clean-sentinel` (`No material findings.`, the same, `no new entry`) and `--material-label` (`findings`, `findings`, `appended`). They keep their range markers and exit-124 marker advance, take the output path from the last stdout line, and annotate parsed findings as `review.findings`. The reflection hook annotates `reflection.applied` (true only when it wrote to its target file). They no longer delete output files, which now live in the run directory. They stop emitting their own telemetry. Their event preflight checks the agentcli binary instead of the provider. Their test suites stub `AGENTCLI_BIN`. The reflection hook's inherited environment marker reaches the provider (FR12). | P0 |
| FR53 | The code-review skill's parallel mode starts `agentcli review --background --json --scenario code-review --source cr-parallel` with the resolved target, and harvests with `agentcli wait <job_id> --timeout <seconds>`. A files-only review with no git target still skips the provider, as today. | P0 |
| FR54 | The stats slash command and the dispatch skill's stats guidance run `agentcli stats`. The retired analyzer script is deleted. The eval harness measures provider dispatches during an attempt as the count of `agentcli runs` whose `ts` falls inside the attempt window and whose `cwd` is the attempt's repository. | P0 |
| FR55 | The skill-gate registry entry binds `agentcli:dispatch` to Bash commands whose first token, after the existing wrapper/env stripping, matches an optional path prefix followed by `agentcli` or `codex`. Every textual reference to the old skill name is updated: in the code-review, spec-audit, brainstorm, goal-generate and orchestrator-mode skills, in the decision-harvesting rule, in the hook documentation, and in the code-review skill's eval files. | P0 |
| FR56 | The old wrapper script, its test and the old skill directory are deleted after FR51 step 6. No script, hook, command file or skill script in the consumer tree invokes `codex exec`, `codex review` or the provider binary directly. Skill and rule prose reaches providers only through `agentcli`. | P0 |

#### N. Validation

| ID | Requirement | Priority |
|----|-------------|----------|
| FR57 | Real smokes on the owner's machine with the installed binary, one per scenario at its profile defaults (second-opinion, code-review, cross-check, expert-persona, delegation), plus one two-turn conversation and one job with `wait`. Each must end with outcome other than `error`/`lost` and a telemetry record. | P0 |
| FR58 | End-to-end: a real commit in a repository during a live Claude Code session triggers the migrated post-commit hook, which reviews through `agentcli` and records `source: hook-post-commit`. | P0 |
| FR59 | Install check: after installing from the GitHub marketplace, `agentcli` resolves from Claude's Bash tool, and after a session start the launcher resolves from a settings hook. | P0 |

#### O. Retention

| ID | Requirement | Priority |
|----|-------------|----------|
| FR60 | `agentcli prune [--older-than <days>] [--dry-run] [--json]` removes the directories of terminal runs whose `ended_at` is older than the given number of days (default 30, or `AGENTCLI_RETENTION_DAYS`). **Strategy:** a run is removed only when its state is terminal with a parseable `ended_at`, its run lock can be taken without blocking, its state is still terminal and old enough when re-read under that lock, and no conversation's active-turn marker names it. Runs with a missing or malformed state are skipped and counted. Telemetry files and conversation records are never removed. After a run is pruned, every reader (`status`, `wait`, `result`, `cancel`, `annotate`, `send` by run id) treats its id as not found (exit 4); a reader that finds its run directory gone mid-read also exits 4. | P0 |
| FR61 | Automatic retention: the plugin's `SessionStart` hook runs `agentcli prune --auto` after `link`. `--auto` prunes at most once per 24 hours per home (a stamp file under a lock), stops after a 3-second time budget and resumes on a later day, prints nothing unless it fails, and is disabled by `AGENTCLI_NO_PRUNE=1`. | P0 |
| FR62 | `status` without a run id reads a per-session index instead of every run directory. **Strategy:** admission appends the run id to the index of the run's session id (`index/sessions/<session id>`; an empty session id uses a fixed key); `status` reads the newest entries of that one index and skips ids whose run directory is gone. A session without an index has no runs. Runs admitted before the index existed are not listed by session. `prune` removes an index file once none of its runs remain. | P0 |
| FR63 | A conversation record keeps the sources of its defaults (`model_source`, `effort_source`, `sandbox_source`), so `send` does not depend on the first turn's run directory. Conversations written before this field existed fall back to the first turn's request when it still exists, else to `profile`. | P0 |

#### P. Agent types, progress and agent feedback

| ID | Requirement | Priority |
|----|-------------|----------|
| FR64 | The mod registers one background agent type per scenario (`<plugin>:second-opinion`, `:code-review`, `:cross-check`, `:expert-persona`, `:delegation`, `:adhoc`), dispatched through the Agent tool. **Strategy:** a `turn.step` hook answers every model request of these subagents with an agentcli job (`--background`, source `agent`, the scenario's profile), never with a Claude model. Every failure answers with a line saying the hand-off failed: the hook's own errors, its `.catch`, a refused tool call inside the subagent, and a registered fallback (cheapest model, one read-only tool, a prompt that makes it say so). The types are offered to the model only once the dispatch skill has loaded in the session, recorded in that session's own state. | P0 |
| FR65 | A SendMessage to such an agent runs `send` on its conversation (agent id → conversation id kept in the plugin store, the 500 most recent); a later turn whose conversation is no longer recorded answers that it cannot resume instead of starting a new one. `:code-review` reads its whole prompt as the target: `uncommitted`, `base <branch>` or `commit <sha>`. Interrupting the agent (Esc, TaskStop) cancels its run. The answer is the output cut at 64 KiB, led by how the run ended when it did not deliver, and closed by `[agentcli run <run_id> · conversation <conversation_id> · <outcome>]`. | P0 |
| FR66 | While a run works, the runner appends what the provider does to `runs/<run_id>/progress.jsonl`: one line per entry `{seq, at, kind, text}`, `kind` one of `command`, `message`, `reasoning`, the text folded to one line of at most 500 bytes. `agentcli progress <run_id> [--from <n>] [--json]` prints the entries numbered from `n`; `--json` prints `{run_id, state, next, entries}`. A malformed complete line is an error; an unterminated last line is left for the next read. | P0 |
| FR67 | An agentcli subagent's row shows its run as it works: the step hook waits in 5-second slices and streams each slice's new progress as thinking (shown, not recorded), with an elapsed-time line after 30 seconds with no new progress. | P0 |
| FR68 | `exec`, `review` and `send` take `--agent-feedback`; `AGENTCLI_AGENT_FEEDBACK=1` sets its default. The run records `agent_feedback` in `request.json` and `status --json`. It needs a session id: without one the run goes ahead with `agent_feedback: false` and one stderr line saying why. | P0 |
| FR69 | A run of the session with `agent_feedback: true`, not started by the agent types and not yet terminal, shows in a band above the prompt (a `ui.render` hook on `AbovePrompt`): one line per run with its scenario, its source, how long it has run and its newest progress entry (FR66). The 15-second poll finds the runs; the band reads their progress again every 5 seconds and drops a run as soon as a read finds it ended. The lines live in the session's own state (`hookRuns`), so a write redraws the band and a reload keeps it. Only the person sees the band: nothing about the run reaches Claude, and the caller that started it delivers its result. **Strategy:** a band rather than an agent row. An agent spawned in a chain that the mod's own timer started steps past the mod's hooks, so a model, not the mod, would answer it; only another plugin's timer can spawn a row the mod answers, and the band keeps the feature in this one plugin. | P0 |
| FR70 | Each run record carries `model_used` and `effort_used`, what the provider reports it ran with, read before the run releases its conversation (for codex, the last `turn_context` of its session file; null when there is none). `stats` adds `usage_by_model`; `stats` and `runs` take `--session-id`. The status line adds the session's cost (FR78), read again when another of its runs ends and after a price refresh that changed the cache (FR77). | P0 |

#### Q. Session cost

| ID | Requirement | Priority |
|----|-------------|----------|
| FR71 | Price cache. `prices.json` under the home directory holds `v` (1), `source` (the URL it was fetched from), `etag` (the source's entity tag, or null), `fetched_at` (when a body was last downloaded), `checked_at` (when the source last answered a refresh, a 304 included), `unit` (`usd_per_1m_tokens`), `models` (provider → model → `{input, cached_input, cache_write, output}`, each a decimal string in US dollars per million tokens) and `unpriced` (provider → sorted list of wanted models the source gave no usable price). It is written atomically, with the home directory created when missing, and read by `stats` and `prices`. **Strategy:** prices are stored resolved: a cache-read price the source omits is stored as the input price, and a cache-write price the source omits, sets to null or sets to 0 is stored as the input price (the provider charges nothing extra for those writes). A file that cannot be parsed, or carries another `v`, is treated as no cache by every reader. | P0 |
| FR72 | `agentcli prices refresh [--max-age <duration>] [--json]` updates the price cache from the LiteLLM price list (`https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json`), or from `AGENTCLI_PRICES_URL` when it is set. Usage errors (exit 2, below) are detected before anything else. **Strategy**, in order: (1) `AGENTCLI_PRICES_URL=off` → exit 0, `ran: false`, `reason: off`. (2) Take `prices.lock` without waiting; held → exit 0, `ran: false`, `reason: busy`. (3) With `--max-age`, a cache whose `checked_at` is younger than the duration → exit 0, `ran: false`, `reason: fresh`; this decision reads the price cache and nothing else. (4) Wanted models, per provider: the provider's catalog (FR76) plus every distinct model of that provider's runs in the whole telemetry (`model_used`, else `model`; empty and `unknown` excluded). A catalog that cannot be read leaves the telemetry models and is reported in `catalog_error`. When no provider has a wanted model, exit 0 with `ran: false`, `reason: nothing_wanted`, no request and no write. (5) GET the source accepting gzip, with a 20-second timeout and a 32 MiB limit on the decoded body. `If-None-Match` carries the cached `etag` only when the cached `source` equals the current one and every wanted model is already in the cache's `models` or `unpriced`. A 304 updates `checked_at` only (`changed: false`, `reason: not_modified`). (6) On a 200, each wanted model takes the entry whose key equals its name exactly and whose `litellm_provider` equals the provider's price namespace (FR76); the models of a provider that declares no catalog, and so no namespace, are unpriced. That entry's `input_cost_per_token` and `output_cost_per_token` must be JSON numbers, otherwise the model is unpriced; `cache_read_input_token_cost` and `cache_creation_input_token_cost` are optional. Every price is parsed from its JSON text as an exact rational and multiplied by 1,000,000, never through binary floating point. (7) When no wanted model got a price, exit 7 and leave the cache untouched. Otherwise write the cache and exit 0 with `ran: true`, `reason: updated`, and `changed` true exactly when `models` or `unpriced` differ from the previous cache. A network error, a timeout, a status other than 200 or 304, a body over the limit or unparseable JSON also exits 7 with the cache untouched. With `--json`, stdout is one object `{sdk_status, exit_code, ran, changed, reason, source, checked_at, priced, unpriced, catalog_error}`, where `priced`, `unpriced` and `checked_at` describe the cache as it stands after the call (`priced` and `unpriced` as sorted `provider/model` lists, empty with no cache, and `checked_at` null with no cache) and an exit 7 carries `sdk_status: prices_unavailable`, `reason: unavailable` and `error`. Without `--json`, stdout is one line `prices: <reason> (<n> priced, <m> unpriced)`, and an exit 7 prints `agentcli: <error>` on stderr instead. A `--max-age` that is not a positive duration such as `24h` or `90m`, an unknown flag or a positional argument exits 2. | P0 |
| FR73 | `agentcli prices [--json]` prints the price cache: its source, `fetched_at`, `checked_at`, each provider's models with their four prices, and the unpriced models. With `--json`, stdout is the cache's fields plus `cached: true`. With no usable cache, it exits 0 and prints `no price cache: run agentcli prices refresh` (`--json`: `{"cached": false}`). | P0 |
| FR74 | Run cost. **Strategy:** for a run with usage whose provider and model (resolved as in FR72 step 4) have cached prices, with `i`, `c`, `w` and `o` its input, cached input, cache-write input and output tokens (a negative count read as 0): the written tokens are `W = w` when `w > 0`, else `W = max(0, i − c)`, because a provider that reports no cache writes is taken to have written every uncached input token, as implicit prompt caching does; the ordinary input is `max(0, i − c − W)`; the cost in US dollars is `(ordinary × input + c × cached_input + W × cache_write + o × output) / 1,000,000`. Reasoning tokens are part of output and are not added again. Every run is priced at its model's current cached prices, short-context standard tier, whatever its date. A run with usage whose model has no cached price is unpriced. Costs are summed as exact rationals and rounded once, half to even, to 6 decimal places when printed. | P0 |
| FR75 | `stats` reports cost for its window and session filter. `usage_totals` and each `usage_by_model` entry add `cost_usd`: the FR74 sum over their priced runs, a decimal string with 6 decimal places, or null when none of their runs with usage is priced. `usage_totals` also adds `cost_complete`, true exactly when no run with usage is unpriced. The top level adds `unpriced_models` (the sorted models of unpriced runs, `unknown` included), `missing_prices` (the sorted subset that is in neither the cache's `models` nor its `unpriced` for the run's provider, `unknown` excluded) and `prices_checked_at` (the cache's `checked_at`, or null with no usable cache). Text output keeps the `tokens` lines without the cost keys and adds `cost: $X.XX`, followed by `(incomplete: <models>)` when not complete, and one `cost <model>: $X.XX` line per priced model, rounded half up to cents; a null cost prints no line. | P0 |
| FR76 | A provider may declare a model catalog: the model names it offers and the namespace its entries use in the price list. **Strategy:** the codex adapter declares the namespace `openai` and reads its catalog by running `codex debug models` with the caller's environment and a 10-second timeout, taking each `models[].slug`. A failure to run it or to parse its output is reported by FR72 and never ends the refresh. | P0 |
| FR77 | The mod refreshes prices without making any hook wait. **Strategy:** every refresh runs `prices refresh --json` inside a `$.clock.after(0, …)` callback, at most one at a time per load of the mod: a refresh requested while one is running is dropped, because the running one reads the same telemetry and source. `session.start` schedules one with `--max-age 24h`. `session.compact` with trigger `manual` or `auto` and no `agentId` schedules one without `--max-age` and returns `next(e)` at once; other triggers and subagent compactions schedule nothing. A stats read (FR78) whose `missing_prices` names a model this load has not yet refreshed for schedules one without `--max-age`, once per model per load; a request that was dropped does not count as that model's one refresh. A refresh that answers `ran: true` and `changed: true` makes the next poll read the session's stats again. A failed refresh is logged once to the debug log until one succeeds. | P0 |
| FR78 | The status line shows the session's cost, read from `stats --session-id <id> --all --json` as in FR70, after the running-jobs count and joined to it by ` · `. **Strategy:** a null `cost_usd` shows nothing for Codex. A complete cost of at least half a cent shows `Codex $X.XX`, rounded half up; a complete cost above zero and below half a cent shows `Codex <$0.01`; a complete cost of zero shows `Codex $0.00`. An incomplete cost shows `Codex ≥$X.XX`, truncated to cents so it stays a lower bound. Rounding works on the decimal string with integer arithmetic, never through binary floating point. | P0 |

### Non-Functional Requirements
- **Performance:** wrapper overhead within the success-metric target. Startup does not read the full telemetry history (only `runs`, `stats` and a `prices refresh` past its `--max-age` gate do; the gate itself reads only the price cache). Telemetry writes are one append. *Assumption: overhead target needs validation by benchmark.*
- **Security:** never `danger-full-access`. Reserved native flags are blocked (FR14). Prompts and outputs stay local under the home directory with user-only permissions (directories 0700, files 0600). Telemetry never records the prompt text. The downloaded price list is untrusted input: its size is capped and its numbers are parsed strictly (FR72).
- **Portability:** linux and darwin on amd64 and arm64. Static binaries with no cgo. POSIX `sh` for shims and the launcher.
- **Reliability:** state transitions are atomic replaces. Telemetry survives crashes mid-write (FR30). A crashed worker is detected as `lost` (FR23). No path can leave a conversation permanently busy.
- **Privacy:** FR44 for the public repository. No telemetry or artifact leaves the machine. The only network request is the unauthenticated GET of the public price list by `prices refresh`, which carries no user data.
- **Accessibility:** not applicable. The product is a CLI, a mod drawing only a status line and toasts and otherwise using Claude Code's own agent rows, and a skill. No visual UI beyond Claude Code's own.
- **Compatibility:** codex-cli 0.159.3 behaviours are the reference (Appendix A). Claude Code with mods and plugin `bin/` support (installed build 2.1.289).

---

## 5. User Experience

### User Flow
1. Install: `claude plugin marketplace add <owner>/agentcli`, then `claude plugin install agentcli@agentcli`. The next session start writes the launcher.
2. Claude asks another agent: it calls the `ask` tool. The tool returns ids at once. Claude keeps working. When the job ends, a toast appears and a new turn brings the outcome and output.
3. Claude follows up: `send` on the same conversation. The answer arrives the same way as turn 2.
4. A hook reviews a commit: it runs `agentcli exec` in the foreground, gets the output path, decides what to surface, and annotates findings.
5. The owner inspects: the status line for the session's cost, `agentcli prices` for the cached prices, `agentcli stats` for the standing report, `agentcli runs --json | jq …` for any new question, `agentcli conversations` and `agentcli status` for live work.

### Design Considerations
- One verb per intent (exec, review, send) and one id per thing (run/job id, conversation id).
- Output paths are always absolute. A foreground run's (and `wait`'s) last stdout line is the output path, and a job admission's last stdout line is the job id, so shell callers stay one-liners.
- Every machine-facing command has `--json`.
- Failure is always explicit: no zero exit with empty output is reported as success (outcome `empty`).

---

## 6. Technical Considerations (High-Level)

### Architecture Impact
New public repository plus migration of the owner's private configuration. Deep modules, each behind a small interface:
- **Runner:** plan → spawn → stream-parse → finalize, for foreground and worker modes alike.
- **Provider adapter:** capabilities, plan building, event parsing, and an optional model catalog. Adding a provider means adding one adapter.
- **Store:** runs, conversations, state files and their locks.
- **Telemetry:** append, fold, `runs` export, `stats`.
- **Prices:** the price cache, the price-list parser and the cost of a run (FR71–FR75), behind a small interface that `stats` and `prices` call.
- **Launcher/link:** install resolution and the launcher file.

The mod and the skill sit outside the binary and contain no dispatch logic.

### Testing Decisions
Test external behaviour, not internals.
- **Primary seam:** the CLI process. Run the built binary against a fake provider placed first on PATH that replays recorded event streams and exit codes. Assert stdout, exit code, run-directory files, state transitions and telemetry lines.
- **Adapter seam:** `--dry-run` plans assert the exact Codex argv/stdin/cwd for every command shape. The event parser is fed the recorded, sanitized Codex streams (exec, exec review, resume, model-not-supported error).
- **Telemetry fold seam:** fold rules (ordering, month boundary, null values, unknown kinds, torn lines) tested as a module.
- **Lifecycle:** job admission, cancel (queued and running), timeout with descendant processes, worker crash → lost, concurrent `send`. Runs on Linux in Docker and on macOS in CI.
- **Mod:** Claude Code's plugin test runner.
- **Prices and cost:** the CLI process against a loopback test server named by `AGENTCLI_PRICES_URL` and a fake provider that answers `debug models`. The cost rule, the decimal handling and the rounding are tested as a module against exact oracles. The mod's cost text and refresh triggers run in the plugin test runner.
- **Consumers:** the existing hook test suites, updated to stub `AGENTCLI_BIN`.
- **Real smokes:** FR57–FR59 on the owner's machine.
- **Prior art:** the PATH-stub pattern used by the current hook suites and by the current wrapper's telemetry test.
- Confidence comes from black-box tests on every exit code and state in FR5/FR22, plus real-provider smokes. Unit tests stay below that seam only where edge logic needs it (fold rules, id validation, flag reservation).

### Dependencies
- codex-cli (reference 0.159.3) on the user's PATH.
- Claude Code with plugin `bin/`, settings hooks and mods (installed 2.1.289).
- GitHub (public repository, Actions for CI on Linux and macOS runners).
- Docker on the developer machine (golang image) for build and test. No language toolchain is installed on the host.
- The LiteLLM price list on GitHub (public, no account), read by `prices refresh`.

### Risks & Mitigations
| Risk | Impact | Mitigation |
|------|--------|------------|
| Codex changes its JSONL event format | High | Parser ignores unknown events; recorded fixtures pin known shapes; provider_version in every record makes drift visible |
| Mods API changes between Claude Code builds | Med | Mod holds no logic; FR49 type check before implementation; CLI fully usable without the mod |
| Plugin `bin/` not on settings-hook PATH | High | Launcher (FR43) and `AGENTCLI_BIN` override; verified by FR59 |
| `dist` force-push publishes a stale build | Med | Serialized CI with HEAD check (FR40); `build_seq` prevents launcher downgrade (FR43) |
| Prompts stored on disk contain sensitive content | Med | User-only permissions; same machine and trust as the provider's own session store; never in telemetry |
| Process-group semantics differ on macOS | Med | Lifecycle tests on a macOS CI runner |
| Hook exceeds its 600-second budget | High | FR26 bounded shutdown; FR30 bounded lock wait |
| The price list changes its schema or renames models | Med | A refresh that prices no wanted model keeps the previous cache and exits 7 (FR72); `prices` shows what is cached and from where |
| The price list carries a wrong price | Med | `prices` shows the source and every cached price; `AGENTCLI_PRICES_URL` can point at a corrected copy |
| Estimated cache writes exceed real ones (a prefix too short to be cached) | Low | The rule is documented in FR74; a provider that reports writes is priced from what it reports |
| The network blocks the price list's host | Low | `AGENTCLI_PRICES_URL` names a mirror or `off`; the last cache keeps working |

---

## 7. Open Questions
None. Deferred execution items, not decisions:
1. ~~Mods API shape on the installed build~~. Deferred to FR49 because it can only be confirmed against the build's type definitions at implementation time; a mismatch is a reported blocker.
2. ~~Mod poll interval~~. Set to 15 seconds (FR47); recalibrating it is a one-value change.

---

## 8. Appendix

### A. Measured facts (2026-10-04, codex-cli 0.159.3, Claude Code 2.1.289)
- Parent flags `-C`, `-s`, `-m`, `-c`, `--json`, `-o` before `review <target>` and before `resume <id> -` work from any cwd.
- `review` with a target rejects a prompt (exit 2) and reports all-zero usage.
- `thread.started.thread_id` from turn 1 is accepted by `resume`; turn 2 recalled turn-1 content.
- With `--json`, a model-not-supported failure arrives as stdout `error` and `turn.failed` events; stderr held only an informational line.
- `turn.completed.usage` carries input, cached input, cache-write input, output and reasoning output tokens.
- Plugin `bin/` is on the Bash tool's PATH; a settings-hook probe found it absent and `CLAUDE_PLUGIN_ROOT` unset.
- The installed-plugins record (version 2) maps `name@marketplace` to entries with scope, install path and version.
- Marketplace plugin sources accept `github` with `ref` and optional `sha`.
- Telemetry baseline: 1,000 records over 24 days, about 300 bytes each; median duration 24.4 s, max 815.9 s.

Measured for session cost (2026-10-08, codex-cli 0.159.3):
- Codex's `input_tokens` includes the cached and cache-write tokens, and its `output_tokens` includes the reasoning tokens: a session file's `total_tokens` equals input plus output, and Codex derives non-cached input as input minus cached. OpenAI's prompt-caching guide states the same partition: each input token is billed at the uncached, cached or cache-write rate, never twice.
- OpenAI prices cache writes at 1.25× the uncached input price and cache reads at 0.1× (0.05× for gpt-6.1-sol) for GPT-5.6 and later; GPT-5.5 and earlier charge nothing extra for writes. In implicit caching mode, the guide's worked example writes every uncached input token of a request to the cache.
- OpenAI list prices for gpt-6.1-sol per million tokens: $2 input, $0.10 cached input, $2.50 cache write, $10 output.
- Under ChatGPT sign-in, every recorded run reports `cache_write_input_tokens: 0` (703 runs with usage), while cached input is 84.7% of all input.
- `codex debug models` prints the model catalog as JSON in under 0.1 s. Of its 10 slugs, two (`gpt-reserve`, `codex-auto-review`) are not in the price list.
- The LiteLLM price list (3.1 MB, about 154 KB gzipped) keys models by name with a `litellm_provider` field, gives prices per token as JSON numbers (for example `1e-07`), and is served with an ETag. Its prices for gpt-6-astra, gpt-6.1-sol and gpt-6-luna match OpenAI's list prices. Its gpt-5.5 entry has no cache-write price.

### B. Glossary
- **run:** one provider invocation; the unit of artifacts, state and telemetry.
- **job:** a run executed in the background; its id is its run id.
- **conversation:** the ordered runs sharing one provider session.
- **turn:** a run's position within its conversation.
- **provider:** an agent CLI (codex now; claude next).
- **adapter:** a provider's implementation inside agentcli.
- **provider session:** the provider's own session/thread id.
- **session id:** the id of the Claude Code session that dispatched a run (distinct from the provider session).
- **scenario:** the caller's label for a run's purpose.
- **profile:** a scenario's default settings.
- **source:** who dispatched the run.
- **outcome:** the classification of a finished run.
- **annotation:** attributes appended to a run after the fact.
- **attrs:** a run's free-form attribute map.
- **price cache:** the local copy of the prices of the wanted models (FR71).
- **wanted models:** the models a provider offers plus those its runs used (FR72).
- **unpriced model:** a wanted model the price list gives no usable price, or a run's model with no cached price.
- **missing price:** a model used by a run that the price cache lists neither as priced nor as unpriced.
- **estimated cache write:** uncached input counted as written to the prompt cache when the provider reports no writes (FR74).
- **session cost:** the FR74 cost of a Claude Code session's runs at API list prices.

### C. Prior art consulted
- JamesPrial/go-plugin-release: orphan release branch for Go plugin binaries.
- cexll/myclaude codeagent-wrapper: Go multi-backend wrapper.
- xunzhimeng/one-code-cli: per-run artifacts, session persisted before launch.
- alexeygrigorev/heru: unified event envelope.
- yelban/codex-orchestrator: detached exec runner.
- nothintoulouse/agentcli: exact-argv tests, Codex format drift.
- openai/codex-plugin-cc: job commands.

### D. Independent review dispositions
Two independent reviews (spec v2 and v3) were applied. Adopted:
- adapter-owned output file;
- reserved passthrough flags;
- launcher in place of a version symlink;
- caller-supplied run ids;
- fold ordering;
- torn-line guard;
- import ids;
- conversation reservation;
- durable prompt handoff;
- lock-based liveness;
- cancellation ownership;
- terminal-state ordering;
- process-group timeouts;
- full exit contract;
- admission JSON;
- forward-only launcher;
- serialized dist publishing;
- an environment-aware plan for the next provider;
- PII scan of dist and ids;
- macOS lifecycle tests.

Cut:
- provider-version caching;
- the profile override file;
- the launcher's cache-glob fallback.

Narrowed: the public contract to the CLI/JSON surface.
