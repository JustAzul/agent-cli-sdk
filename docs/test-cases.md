# Verification Checklist: agentcli (`agentcli`)

**Date**: 2026-10-04 (updated 2026-10-08: session cost)
**Status**: Draft

Unless stated otherwise:
- `AGENTCLI_HOME` points at a fresh temporary directory.
- A fake `codex` is first on PATH. It replays a named recorded event stream (sanitized fixtures: `exec-ok`, `review-ok`, `resume-ok`, `error-400`), writes the `-o` file, and exits with a scripted code.
- `AGENTCLI_PRICES_URL=off`, so no case reaches the network. Price cases point it at a loopback test server that serves a fixture price list.
- "Real" cases use the installed codex-cli and are the FR57–FR59 smokes.

---

## Happy Path

- [ ] **exec foreground (FR1, FR5, FR13, FR15, FR31):** fake `exec-ok` → `agentcli exec --scenario second-opinion "q"`.
  - Exit 0; the last stdout line is the absolute path of `runs/<run_id>/output.md`, which holds the fixture's final message.
  - `state.json` is `done`.
  - One `run` record with `command: exec`, `turn: 1`, `outcome: ok`, `usage.output_tokens` equal to the fixture value, `model: gpt-6.1-sol`, `model_source: profile`, `provider_session_id` equal to the fixture's thread id.
- [ ] **exec from stdin (FR1):** `printf 'q' | agentcli exec -` → `prompt.md` equals `q` and the fake received `q` on stdin.
- [ ] **exec from file (FR1):** `agentcli exec --prompt-file p.md` → `prompt.md` equals the file content.
- [ ] **dry-run plan (FR8, FR13):** `agentcli exec --dry-run --cwd /work/repo --model m --effort high --sandbox read-only "q"` → the JSON plan's argv is exactly `codex exec -C /work/repo -s read-only -m m -c model_reasoning_effort=high --json -o <path> -`, the stdin source is the prompt, and no run directory, conversation or telemetry exists afterwards.
- [ ] **review plan (FR2, FR13):** `agentcli review --base main --dry-run` → argv ends with `review --base main`, stdin is empty, and there is no `-` tail.
- [ ] **send resumes (FR3, FR13, FR18, US2):** after an `exec` whose fixture thread id is T, `agentcli send <conversation_id> "follow"`.
  - argv ends with `resume T -`.
  - The record has `turn: 2` and the same `conversation_id`.
  - The conversation lists both run ids in order.
- [ ] **send by run id (FR3):** `agentcli send <turn-1 run_id> "x"` → resolves to the same conversation, turn 2.
- [ ] **job admission and wait (FR6, FR7, FR21, US4):** `agentcli exec --background --json "q"`.
  - Exit 0 within 10 s with `{conversation_id, run_id, state: running, run_dir, output_path}`.
  - `agentcli wait <run_id>` from another shell exits 0 and its last stdout line equals `output_path`.
  - The record has `background: true`.
- [ ] **job last line (FR6):** `agentcli exec --background "q"` (no `--json`) → the last stdout line equals the run id.
- [ ] **status listing (FR7, FR62):** two runs under `--session-id S` and one under `S2` → `agentcli status --session-id S --json` lists exactly the two `S` runs, newest first.
- [ ] **prune summary (FR60):** two runs, one finished 40 days ago and one fresh → `agentcli prune --json` exits 0 with `removed: 1`, `kept: 1`, `skipped: 0`, `older_than_days: 30` and `bytes_freed` greater than 0. The old run directory is gone, the fresh one stays, and the text form prints `removed 1, kept 1, skipped 0, freed <n> bytes`.
- [ ] **prune retention window (FR60):** `--older-than 60` keeps the 40-day-old run, `--older-than 10` removes it, `AGENTCLI_RETENTION_DAYS=60` keeps it, and the flag wins over the variable. `--older-than 0` removes every finished run.
- [ ] **prune dry run (FR60):** `prune --dry-run` prints `would remove 1, …` (JSON: `dry_run: true`, `removed: 1`), removes nothing and creates no lock file in any run directory.
- [ ] **prune leaves history alone (FR60):** after a prune, the telemetry records and the conversation files are byte-for-byte unchanged.
- [ ] **index on admission (FR62):** `exec`, `review`, `send` and the two `--background` forms with `--session-id S` → `index/sessions/S` lists their run ids in admission order, one per line. A run with no session id is listed in `index/sessions/_`, and `status` with no session id lists it.
- [ ] **session status reads one index (FR62):** a run that no index lists is not shown by `status --session-id X`, even when its request says session `X`. A session with no index lists no runs.
- [ ] **conversation sources (FR63):** `exec --scenario second-opinion --model m1` → the conversation's `defaults` hold `model_source: flag`, `effort_source: profile`, `sandbox_source: profile`.
- [ ] **send after the first turn is pruned (FR60, FR63):** prune removes turn 1 of a conversation started with `--model m1` → `send <conversation_id>` runs turn 2 with `model: m1`, `model_source: flag`, `effort_source: profile`. `send <turn-1 run id>` exits 4.
- [ ] **auto prune (FR61):** `prune --auto` on a home with an old run → exit 0, nothing on stdout or stderr, the old run is gone, and `prune.stamp` holds the time of the pass.
- [ ] **hook command (FR61):** the plugin's SessionStart hook command is `"${CLAUDE_PLUGIN_ROOT}"/bin/agentcli link --quiet; "${CLAUDE_PLUGIN_ROOT}"/bin/agentcli prune --auto` with timeout 5.
- [ ] **result (FR7):** `agentcli result <run_id>` → prints the content of `output.md`.
- [ ] **conversations (FR7):** after exec + send → `agentcli conversations --json` shows the conversation with provider `codex`, 2 turns, status `idle` and `resumable: true`.
- [ ] **attrs at dispatch (FR4, FR31, US6):** `--attr ticket=2056 --attr-json review.findings='{"total":1}'` → the record's `attrs` equals `{"ticket":"2056","review.findings":{"total":1}}`.
- [ ] **annotate after the fact (FR32, FR33, US6):** `agentcli annotate <run_id> --attr acted=yes` → `agentcli runs --json` shows the run with `attrs.acted == "yes"` alongside the dispatch attrs.
- [ ] **annotate by output path (FR32):** `agentcli annotate <output_path> --attr k=v` → annotation recorded for that run id.
- [ ] **clean outcome (FR28, US3):** the fixture output is `No material findings.` with `--clean-sentinel 'No material findings.'` → outcome `clean`.
- [ ] **material label (FR28):** the fixture output is a findings list with `--material-label findings` → outcome `findings`.
- [ ] **profiles (FR38):** `--scenario delegation` → plan has `-s workspace-write`, `-c model_reasoning_effort=medium`, `-m gpt-6.1-sol`. `--scenario delegation --effort high` → effort high with `effort_source: flag`.
- [ ] **unknown scenario (FR38):** `--scenario claude-md-update` → no profile applied, record `scenario: claude-md-update`, sources `provider-default` unless flags are given.
- [ ] **stats (FR35, US7):** with mixed records → `agentcli stats --all --json` has every key `total, empty, span, by_source, by_source_status, instrumentation, reliability, outcomes, duration_ms, findings, review_findings_proxy, window_days`, plus `by_provider`, `usage_totals`, `usage_by_model`, `skipped`, `unpriced_models`, `missing_prices` and `prices_checked_at`, and no other key.
- [ ] **version (FR10):** `agentcli version --json` → has `version`, `source_commit`, `build_seq` (integer > 0 for CI builds) and `platform`.
- [ ] **link fresh (FR43, US5):** no launcher present → `agentcli link`.
  - It creates `~/.local/bin/agentcli` (directory created if missing), which executes the resolved install's `bin/agentcli` and carries its `build_seq`.
  - `~/.local/bin/agentcli version` matches.
- [ ] **shim platform selection (FR41):** on linux/amd64 → `bin/agentcli version --json` reports `platform: linux/amd64`. The same holds on the macOS CI runner for darwin.
- [ ] **mod ask (FR46, FR47, US1):** in a session with the mod, Claude calls `ask {prompt}`.
  - It returns `{conversation_id, run_id}` at once.
  - Within one poll interval after the job ends, a toast appears and a turn is submitted whose text starts `agentcli job <run_id>` and contains the outcome and the output inline (≤ 8 KiB).
- [ ] **mod send (FR46):** `send {conversation_id, prompt}` → a job for turn 2, with a notice on completion.
- [ ] **mod jobs (FR46):** `jobs {action: "list"}`, `{action: "status", run_id}`, `{action: "result", run_id}`, `{action: "cancel", run_id}` each return the CLI's JSON or content.
- [ ] **mod status line and command (FR48):** two running jobs → the status line shows 2. `/agentcli-jobs` prints the session's recent jobs while Claude is mid-turn.
- [ ] **provider version (FR17):** fake `codex --version` prints `codex-cli 9.9.9` → the record's `provider_version` is `codex-cli 9.9.9`. A fake whose version flag exits 1 → `provider_version: null`, and the run is otherwise unaffected.
- [ ] **terminal ordering (FR22):** a test observer polls a job; at the first poll where `state.json` is terminal, `output.md` already has its final bytes and the conversation's marker is already cleared. The telemetry record appears after the terminal state, never before.
- [ ] **job survives its launcher (FR25):** start a job from a shell, then kill that shell's whole process group → the job still reaches `done`, and `wait` from a new shell exits 0.
- [ ] **namespaced attrs (FR36):** `--attr-json review.findings='{"total":2}'` → stored under the literal key `review.findings`, not nested as `review → findings`.
- [ ] **marketplace manifest (FR39):** `claude plugin validate` on the repository root passes. The manifest lists one plugin `agentcli` with source `github`, `ref: dist` and no `sha`.
- [ ] **dist content (FR40, FR42):** the published `dist` tree contains the plugin manifest without `version`, `bin/agentcli`, four binaries, `SHA256SUMS` (all four checksums verify), the skill, the mod, commands, and a hooks file whose SessionStart runs `agentcli link --quiet; agentcli prune --auto`.
- [ ] **repository identity (FR45):** `git log --format='%an <%ae>'` on the public repo shows only the owner's global git identity. LICENSE is MIT with the owner's name.
- [ ] **mods API check (FR49):** before mod code is written, the installed build's mod type definitions are read, and the registration, tool, timer, toast, prompt-submit, status and store calls used by FR46–FR48 exist with the expected shapes. Any mismatch is reported as a blocker.
- [ ] **dispatch skill (FR50):** the skill text covers scenarios/profiles, tools vs CLI, foreground vs job, continuing conversations, reading results, attribution and open disagreement, no patch application outside delegation, and cost. The carried-over trigger evals pass with the renamed skill.
- [ ] **real per-scenario smokes (FR57):** installed binary, real codex → one run each of second-opinion, code-review (`review --uncommitted` on a scratch repo), cross-check, expert-persona, delegation (scratch repo) at profile defaults. Each outcome is not `error`/`lost`, each has a telemetry record with non-null `usage` (except review), and `error_excerpt` is null.
- [ ] **real conversation and job (FR57):** real exec, then real `send` → turn 2 references turn-1 content. A real `--background` job, then `wait`, exits 0.
- [ ] **post-commit E2E (FR52, FR58):** live session, a real commit in a scratch repo → the migrated hook surfaces a review or stays silent on clean, and `agentcli runs --json` shows a record with `source: hook-post-commit`, `scenario: code-review`, `model: gpt-6.1-sol`.
- [ ] **cutover order (FR51):** the migration log shows the steps in order (install → skill → hooks → parallel review → analyzer/command/harness → legacy import + archive → deletion), each with its verification evidence recorded before the next starts.
- [ ] **parallel review (FR53, US4):** the code-review skill in parallel mode on a scratch branch → one job with `source: cr-parallel`, `scenario: code-review`; harvest through `wait` returns the provider exit and output path. A files-only target starts no job.
- [ ] **stats command and harness (FR54, US7):** the stats slash command prints `agentcli stats` output. The eval harness counts as dispatches exactly the runs whose `ts` is inside the attempt window and whose `cwd` is the attempt's repository; a run from another cwd in the same window is not counted.
- [ ] **skill gate (FR55):** in a session without the dispatch skill loaded, each of `agentcli exec "q"`, `nohup agentcli exec "q"`, `FOO=1 agentcli exec "q"`, `/abs/path/agentcli exec "q"` and `codex exec "q"` triggers the precondition naming `agentcli:dispatch`. `echo agentcli` does not.
- [ ] **old names gone (FR55, FR56):** a text search for the old skill name across the listed skills, rules, hook docs and eval files returns no stale reference. A text search for `codex exec`/`codex review` invocations across hook scripts, hook libraries, scripts, commands and skill scripts returns zero (success metric 1).
- [ ] **install check (FR59, US5):** after the marketplace install, a Bash tool `command -v agentcli` resolves inside the plugin. After a session start, a settings-hook probe resolves `~/.local/bin/agentcli`.

---

## Agent types, progress and agent feedback

- [ ] **agent type answers (FR64, FR67):** in a session with the plugin, after the dispatch skill loads, Claude calls `Agent(subagent_type: "agentcli:second-opinion", prompt: "q")`.
  - A background agent row appears and streams the run's progress as thinking.
  - The completion notification carries the output and the `[agentcli run … · conversation … · ok]` trailer.
  - No Claude model request is made for the subagent; telemetry has one run with source `agent`.
- [ ] **gate (FR64):** before the dispatch skill loads, `Agent(subagent_type: "agentcli:adhoc")` is refused as an unknown type; after it loads, the call is accepted. Loading it in another session does not close this one.
- [ ] **follow-up (FR65):** a SendMessage to a finished agentcli agent runs `send` on its conversation; the provider answers with the earlier turn's context.
- [ ] **cancel (FR65):** TaskStop on a running agentcli agent → its run ends `cancelled` (exit 130).
- [ ] **hand-off failure (FR64):** an agentcli failure (exit 3, busy) → the answer starts with `agentcli: the hand-off to the agent failed` and no model request is made.
- [ ] **progress (FR66):** fake `review-ok` → `agentcli progress <run_id> --json` lists the two commands and the final message in order, `next: 3`; `--from 3` lists none; an unknown run exits 4.
- [ ] **agent feedback flag (FR68):** `--agent-feedback --session-id s` → `agent_feedback: true` in `request.json` and `status --json`; `AGENTCLI_AGENT_FEEDBACK=1` likewise; with no session id the run exits 0, `agent_feedback: false`, and stderr says it was ignored.
- [ ] **flagged run shown (FR69):** a hook starts a foreground `exec --agent-feedback` in the session → within one poll a line `agentcli · <scenario> · <source> · <elapsed> · <newest step>` appears above the prompt and follows what the run does; within 5 seconds of the run ending the line is gone. A run started by the agent types, or without the flag, gets no line. No agent row appears, no model request is made, and Claude receives nothing about the run.
- [ ] **model used (FR70):** a run on the provider's default model records `model: null` and `model_used` equal to the model in the provider's session file; `stats --session-id s --json` counts only that session and lists its tokens under `usage_by_model`.

---

## Session cost and prices

Fixtures for this section:
- **Price list:** `testdata/prices/` serves a LiteLLM-shaped file with fictitious models. All have `litellm_provider: openai` except the last two.
  - `m-sol`: input `2e-06`, cache read `1e-07`, cache creation `2.5e-06`, output `1e-05`.
  - `m-old`: input `5e-06`, cache read `5e-07`, output `3e-05`; no cache creation.
  - `m-bare`: input `1e-06`, output `4e-06`; no cache fields.
  - `m-zero`: input `3e-06`, cache read `3e-07`, cache creation `0`, output `1.2e-05`.
  - `m-half`: input `5e-07`, output `5e-07`.
  - `m-text`: input as the string `"2e-06"`, output `1e-05`.
  - `chatgpt/m-sol` (`litellm_provider: chatgpt`, prices null) and `m-azure` (`litellm_provider: azure`).
- **Catalog:** `FAKECODEX_MODELS` lists `m-sol`, `m-old`, `m-bare` and `m-hidden`.
- **Server:** the test server answers `ETag: "e1"` and returns 304 when `If-None-Match: "e1"` arrives. It records every request it receives.

Expected cached prices (US dollars per million tokens):
- `m-sol`: `{input "2", cached_input "0.1", cache_write "2.5", output "10"}`.
- `m-old`: `{5, 0.5, 5, 30}`.
- `m-bare`: `{1, 1, 1, 4}`.
- `m-zero`: `{3, 0.3, 3, 12}`.

### Happy path

- [ ] **refresh writes the cache (FR71, FR72, FR76):** one telemetry run on `m-zero` → `agentcli prices refresh --json` exits 0.
  - The JSON has `ran: true`, `reason: updated` and `changed: true`.
  - `priced` is `[codex/m-bare, codex/m-old, codex/m-sol, codex/m-zero]` and `unpriced` is `[codex/m-hidden]`.
  - `prices.json` holds the expected prices above, `etag` `"e1"` and `source` equal to the server URL.
  - The file has mode 0600, and no `m-azure`, `chatgpt/m-sol` or `m-half` entry.
  - The server saw one GET with no query string, no `Authorization` header and no cookie.
  - The text form (no `--json`) prints exactly `prices: updated (4 priced, 1 unpriced)`.
- [ ] **prices shows the cache (FR73):** after the refresh, `agentcli prices --json` → `cached: true` plus the cache's fields. The text form lists each model with its four prices and the unpriced models.
- [ ] **run cost with estimated writes (FR74, FR75):** one run on `m-sol` with usage `input 1,000,000, cached 800,000, cache_write 0, output 10,000`.
  - `stats --all --json` gives `usage_by_model.m-sol.cost_usd` `"0.680000"`: 200,000 estimated written tokens at $2.50, 800,000 cached at $0.10 and 10,000 output at $10.
  - `cost_complete` is `true`.
- [ ] **reported writes win (FR74):** the same run with `cache_write 50,000` → `"0.605000"`: 150,000 ordinary at $2, 800,000 cached, 50,000 written and the output.
- [ ] **omitted cache prices (FR71, FR74):**
  - `m-old` with `input 100,000, cached 60,000, output 1,000` → `"0.260000"`, the writes at the input price.
  - `m-bare` with `input 10,000, cached 4,000, output 500` → `"0.012000"`.
- [ ] **exact totals (FR74, FR75):** the `m-sol`, `m-old` and `m-bare` runs above together → `usage_totals.cost_usd` is `"0.952000"`. That equals the independent sum of the three oracles, with no rounding before the sum.
- [ ] **reasoning not double-counted (FR74):** the `m-sol` run with `reasoning_output_tokens 5,000` added → its cost is unchanged.
- [ ] **hook runs count (FR75, US8):** runs with `source` `hook-stop`, `hook-post-commit` and `cli`, all under session `S` → `stats --session-id S --all --json` prices all three.
- [ ] **stats text (FR75):** for the three runs above:
  - the `tokens:` lines carry no cost key;
  - the output has `cost: $0.95`, plus `cost m-sol: $0.68`, `cost m-old: $0.26` and `cost m-bare: $0.01`.
- [ ] **status line cost (FR78, US8):** stats answers `cost_usd "0.680000"` with `cost_complete: true` and one job running → the status line is `1 job running · Codex $0.68`. With no job running, it is `Codex $0.68`.
- [ ] **status line rounding (FR78):**

  | `cost_usd` | complete? | Status line |
  |---|---|---|
  | `"52.940726"` | yes | `Codex $52.94` |
  | `"1.005000"` | yes | `Codex $1.01` |
  | `"0.005000"` | yes | `Codex $0.01` |
  | `"0.004999"` | yes | `Codex <$0.01` |
  | `"0.000000"` | yes | `Codex $0.00` |
  | `"52.949999"` | no | `Codex ≥$52.94` |
  | `"0.004999"` | no | `Codex ≥$0.00` |
  | null | — | nothing for Codex |

- [ ] **refresh on compaction (FR77, US8):**
  - A `session.compact` with trigger `manual`, then one with `auto`, both with no `agentId` → each hook resolves before its refresh process finishes, and each schedules one `prices refresh --json` with no `--max-age`.
  - The triggers `precompute` and `plugin`, and any compaction with an `agentId`, schedule none.
- [ ] **refresh on session start (FR77):** `session.start` → one `prices refresh --max-age 24h --json` runs from a timer callback, not inside the hook.
- [ ] **re-read after a change (FR70, FR77):** a refresh answering `ran: true, changed: true` → the next poll runs `stats` again, even though no run ended.
- [ ] **live cost of a running run (FR79):** a background run with provider session `T`, kept running by the fake, and the price cache holding `m-sol`. `CODEX_HOME/sessions/…/rollout-…-T.jsonl` has a `turn_context` with model `m-sol`, then `token_count` lines stamped after the run's start; the newest totals are `input 1,000,000, cached 800,000, cache_write 0, output 10,000, reasoning 2,000`. → `progress <run_id> --json` has `usage` with those counts and `cost_usd` `"0.680000"`.
- [ ] **resumed conversation subtracts what came before (FR79):** the same file also holds a `token_count` stamped before the run's start with totals `input 400,000, cached 300,000, output 5,000`, and the newest totals are `input 1,400,000, cached 1,100,000, output 15,000` → `usage` is `input 1,000,000, cached 800,000, output 10,000` and `cost_usd` is `"0.680000"`.
- [ ] **band shows the cost (FR80):** progress answers `cost_usd "0.123456"` for a flagged run that has run 95 s and whose newest step is `Reading the diff` → the band line is `agentcli · code-review · hook-stop · 1m 35s · $0.12 · Reading the diff`. With `"0.004000"` it shows `<$0.01`, and with null there is no cost part.

### Edge cases

- [ ] **unpriced and missing (FR75):**
  - Runs on `m-sol`, `m-hidden`, `m-new` (in no cache list) and one with no model → `cost_complete: false`.
  - `unpriced_models` is `[m-hidden, m-new, unknown]` and `missing_prices` is `[m-new]`.
  - `usage_totals.cost_usd` covers the `m-sol` run only.
- [ ] **nothing priced (FR75, FR78):** with no cache, or only unpriced runs → `usage_totals.cost_usd` is null and the status line shows nothing for Codex. With no cache, `prices_checked_at` is null and `missing_prices` lists every model used except `unknown`.
- [ ] **no usage (FR75):** a window whose runs all have null usage → `cost_usd` null and `cost_complete: true`.
- [ ] **exact literals (FR72):** `m-sol`'s `1e-07` cache read is stored as `"0.1"`, not as a binary-float approximation.
- [ ] **half to even (FR74):** with `m-half` wanted, one run with `input 1` → `"0.000000"`; one with `input 3` → `"0.000002"`.
- [ ] **non-number price (FR72):** `m-text` wanted → listed as unpriced.
- [ ] **wrong namespace ignored (FR72):** a wanted `m-azure` → unpriced, even though the list has an entry under that key. The `chatgpt/m-sol` key never matches `m-sol`.
- [ ] **conditional request (FR72):** a refresh, then another → the second request carries `If-None-Match: "e1"`.
  - It answers `ran: true, changed: false, reason: not_modified`.
  - `checked_at` advances and `fetched_at` and `models` are unchanged.
- [ ] **new model forces a full GET (FR72):** after a refresh, a telemetry run on `m-old2` (not in the cache) → the next request carries no `If-None-Match`, and `m-old2`, absent from the list, ends up in `unpriced`.
- [ ] **source change forces a full GET (FR72):** the cache's `source` differs from `AGENTCLI_PRICES_URL` → no `If-None-Match`, and the new `source` is written.
- [ ] **fresh cache skips everything (FR72):** with `checked_at` 1 hour old, `prices refresh --max-age 24h --json` → `ran: false, reason: fresh`. The server receives no request, and `FAKECODEX_RECORD` shows `codex` was not run.
- [ ] **stale cache refreshes (FR72):** with `checked_at` 25 hours old, `--max-age 24h` → it refreshes.
- [ ] **catalog fails (FR72, FR76):** `FAKECODEX_MODELS` unset → exit 0. Only the telemetry models are wanted, and `catalog_error` is non-empty.
- [ ] **nothing wanted (FR72):** `FAKECODEX_MODELS` unset and no telemetry → exit 0, `ran: false, reason: nothing_wanted`. The server receives no request and no `prices.json` is written.
- [ ] **ran false describes the cache (FR72):** `reason: fresh` on a cache that has 4 priced and 1 unpriced model → `priced` and `unpriced` list them, and `checked_at` equals the cache's. `reason: off` with no cache → empty lists and `checked_at: null`.
- [ ] **usage errors come first (FR72):** `AGENTCLI_PRICES_URL=off agentcli prices refresh --max-age soon` → exit 2, not `reason: off`.
- [ ] **corrupt cache (FR71):** `prices.json` holding `{` or `"v": 2` → `stats` reports null costs and `prices_checked_at: null`; `prices` reports `cached: false`; `refresh` makes a full GET and replaces the file.
- [ ] **usage errors (FR72):** `prices refresh --max-age soon`, `--max-age -1h`, an unknown flag, or a positional argument → exit 2 with `sdk_status: usage_error`.
- [ ] **no live usage yet (FR79):** no session file, a session file with no `token_count` after the run's start, or a conversation with no provider session id → `progress --json` exits 0 with `usage: null` and `cost_usd: null`.
- [ ] **the start second belongs to the previous turn (FR79):** the session file holds the previous turn's last `token_count` stamped 0.5 s after the run's `started_at` (totals `input 400,000, cached 300,000, output 5,000`) and the run's own one 3 s after (`input 1,400,000, cached 1,100,000, output 15,000`) → `usage` is `input 1,000,000, cached 800,000, output 10,000` and `cost_usd` is `"0.680000"`.
- [ ] **unreadable conversation record (FR79):** the run's conversation file holds `{` → `progress --json` exits 0 with `usage: null`, `cost_usd: null` and one `agentcli: warning:` line naming the run.
- [ ] **ended run reports no live usage (FR79):** a run that has ended (`done`) whose session file holds `token_count` lines, some of them from a later turn → `progress --json` has `usage: null` and `cost_usd: null`.
- [ ] **unreadable session file (FR79):** the session file path is a directory → `progress --json` exits 0 with `usage: null`, `cost_usd: null` and one `agentcli: warning:` line on stderr; with no session file at all, stderr stays empty.
- [ ] **live usage without a price (FR79):** the session file's model is `m-hidden`, or there is no usable price cache → `usage` is set and `cost_usd` is null.
- [ ] **model falls back to the request (FR79):** a session file with `token_count` lines but no `turn_context`, for a run requested with `--model m-sol` → `cost_usd` uses `m-sol`'s prices.
- [ ] **torn session file (FR79):** the session file's last line is cut mid-object → it is ignored, and `usage` comes from the newest complete `token_count`.
- [ ] **compaction during a refresh (FR77):** a compaction arrives while a `--max-age 24h` refresh is still running → no second process starts while it runs. When it ends (even with `reason: fresh`), exactly one queued `prices refresh --json` without `--max-age` starts.
- [ ] **requests coalesce (FR77):** two compactions and a missing-price request arrive while one refresh runs → exactly one queued refresh runs after it, not three.
- [ ] **missing price triggers once (FR77):** stats keeps answering `missing_prices: [m-new]` → exactly one refresh runs for `m-new` in this load.
- [ ] **queued request survives an unchanged refresh (FR77):** `missing_prices: [m-new]` arrives while a refresh that will answer `changed: false` is running → when it ends, the queued refresh runs. If that one answers `changed: true`, the next poll reads stats again, with no new run ending in between.

### Failure and error handling

- [ ] **list unavailable (FR5, FR72):** each of these cases → exit 7, `sdk_status: prices_unavailable`, `reason: unavailable`, a non-empty `error`, and `prices.json` byte-identical (or still absent). Without `--json`, stdout is empty and stderr has one `agentcli: …` line:
  - the server is closed (connection refused);
  - the server answers 500;
  - the body is invalid JSON;
  - the body has no wanted model;
  - the body exceeds `AGENTCLI_TEST_PRICES_MAX_BYTES`;
  - the server stalls past `AGENTCLI_TEST_PRICES_TIMEOUT_MS`.
- [ ] **refresh switched off (FR72):** `AGENTCLI_PRICES_URL=off` → exit 0, `ran: false, reason: off`. No request is made and no file is created.
- [ ] **lock held (FR72):** another process holds `prices.lock` → exit 0, `ran: false, reason: busy`, and the cache is unchanged.
- [ ] **failed refresh in the mod (FR77):** two refreshes in a row exit 7 → one debug log line, not two. After one succeeds, the next failure logs again.

### Idempotency

- [ ] **refresh twice (FR72):** two refreshes with no change upstream → the second has `changed: false`, and `prices.json` differs only in `checked_at`.
- [ ] **reload (FR77):** the mod reloads 3 times within an hour of a refresh → each `session.start` refresh answers `reason: fresh`, and the server receives no request.

### Performance and limits

- [ ] **refresh over a large history (FR72):** with 20,000 telemetry records, `prices refresh` completes. Record its wall time as a baseline.
- [ ] **long session file (FR79, NFR Performance):** a 50 MB session file whose run events sit in its last 20 KB → `progress --json` returns the right `usage`. Record its wall time as a baseline; the file is read from its end.
- [ ] **the age gate reads nothing else (FR72, NFR Performance):** with a fresh cache and an unreadable telemetry directory, `prices refresh --max-age 24h` exits 0 with `reason: fresh`.

## Edge Cases

- [ ] **two prompt sources (FR1):** `agentcli exec --prompt-file p.md "q"` → exit 2 before spawning; the fake was not invoked.
- [ ] **no prompt (FR1):** `agentcli exec` → exit 2.
- [ ] **review with prompt (FR2):** `agentcli review --base main "focus"` → exit 2 before spawning, with a message that review targets take no prompt.
- [ ] **review with two targets (FR2):** `--base main --uncommitted` → exit 2.
- [ ] **cwd outside git (FR13):** `--cwd` set to a non-git temp dir → the plan contains `--skip-git-repo-check`. Inside a git repo it does not.
- [ ] **never ephemeral (FR13):** every dry-run plan → no `--ephemeral`.
- [ ] **reserved passthrough (FR14):** each of `-- -o x`, `-- --json`, `-- -C /x`, `-- -m m`, `-- -s read-only`, `-- --dangerously-bypass-approvals-and-sandbox`, `-- --approve-for-me`, `-- --ephemeral`, `-- -c model=x`, `-- -c model_reasoning_effort=low`, `-- -c sandbox_permissions=[]` → exit 2 with the offending flag named.
- [ ] **allowed passthrough (FR14):** `-- -c features.web_search=true --add-dir /tmp/x` → present in the plan after the SDK's flags.
- [ ] **danger sandbox (FR14):** `--sandbox danger-full-access` → exit 2.
- [ ] **run-id validation (FR9):** `--run-id 'a b'`, `--run-id ..`, `--run-id .hidden`, `--run-id -x` → exit 2 each, with no directory created outside `runs/`. `--run-id ok.id-1` → accepted and used as the directory name. Reusing it → exit 2.
- [ ] **provider mismatch on send (FR20):** a codex conversation → `agentcli send <conversation_id> --provider other "x"` exits 2. Omitting `--provider` uses `codex`.
- [ ] **link without install record (FR43):** no installed-plugins record for `agentcli@agentcli` → `agentcli link` exits 4 with a message. `agentcli link --target /work/agentcli-shim` writes a launcher executing that path with the running binary's `build_seq`.
- [ ] **stats skipped counts (FR33):** one unknown-kind, one unknown-version and one garbage line → `stats --json` has `skipped: {unknown_kind: 1, unknown_version: 1, unparseable: 1}`. `runs` prints one stderr line with the same counts.
- [ ] **stats default window (FR35):** records at 3 days and at 10 days old → `agentcli stats --json` counts only the 3-day record and reports `window_days: 7`.
- [ ] **background with stdin (FR21):** `printf 'long prompt' | agentcli exec --background -` → `prompt.md` holds the full text before the worker starts, and the fake receives it.
- [ ] **concurrent send (FR19, US2):** while a turn-2 job is `running`, `agentcli send <conversation_id> "x"` → exit 3 naming the active run id. The same holds while it is `queued`.
- [ ] **simultaneous admissions (FR19):** two `send` admissions launched at the same instant on an idle conversation → exactly one is admitted and the other exits 3. Turn numbers have no duplicates or gaps.
- [ ] **not resumable (FR20):** turn 1 fails before any `thread.started` → `agentcli send <conversation_id> "x"` exits 6 with the reason.
- [ ] **per-turn override isolation (FR20):** turn 2 with `--effort low` → turn 3 without flags uses the first-turn default effort.
- [ ] **follow-up cwd (FR20):** turn 1 in `/work/a`; `send` issued from `/work/b` → the plan's `-C /work/a`.
- [ ] **empty output (FR28):** exit 0 with an empty `-o` file → outcome `empty`; the exit code passes through as 0.
- [ ] **review usage zero (FR15):** fixture `review-ok` with all-zero usage → record `usage: null`.
- [ ] **unknown events (FR15):** a fixture with an extra unknown event type and one garbage line → the run completes normally and `state.json` has `unparsed_events: 1`.
- [ ] **month boundary fold (FR30, FR33):** a run with `ts` on 2026-09-30T23:59:50Z whose record is appended on 2026-10-01 lands in `2026-10.jsonl`. Its annotation lands in `2026-10.jsonl` too. A `--days` window starting in September returns the run with folded attrs, and the reader opened both month files.
- [ ] **security permissions (NFR Security):** after any run → the home, `runs/<id>` and `conversations` directories are mode 0700 and their files 0600.
- [ ] **prompt never in telemetry (NFR Security):** a prompt containing a unique marker string → no telemetry file contains the marker. `prompt.md` does.
- [ ] **null is a value (FR33):** annotate `--attr-json k=null` after `k=1` → the folded `attrs.k` is `null` and the key is present.
- [ ] **unknown kind/version (FR33):** inject `{"v":2,"kind":"run",...}` and `{"v":1,"kind":"note"}` → both skipped; reader output reports a skipped count of 2.
- [ ] **torn tail (FR30):** a month file ending mid-record without a newline → the next append starts on a new line, the torn line is counted as unparseable, and the new record parses.
- [ ] **launcher foreign file (FR43):** `~/.local/bin/agentcli` is a non-launcher file (e.g. a pipx entry point) → `link` leaves it untouched and prints why; with `--quiet` it prints nothing. Exit 0 in both.
- [ ] **launcher opt-out (FR43):** `AGENTCLI_NO_LINK=1 agentcli link` → no file written.
- [ ] **unsupported platform (FR41):** shim run with `uname` stubbed to `FreeBSD` → prints the supported platforms, exit 70.
- [ ] **arch aliases (FR41):** `uname -m` stubbed to `aarch64`, then `arm64` → both select the `arm64` binary. `x86_64` and `amd64` both select `amd64`.
- [ ] **build_seq from full history (FR40):** the CI checkout is full-history, and the published binary's `build_seq` equals `git rev-list --count` of the built commit.
- [ ] **PII guard (FR44):** the test generates, at run time, a tracked file holding a webmail address → the CI test fails. The same for a generated `/home/<user>/…` path and for a UUID not on the fixture allowlist. Noreply/example emails pass. No such sample is committed literally.
- [ ] **hook keeps artifacts (FR52):** a migrated review hook gets a clean review → the hook stays silent and the run's `output.md` still exists afterwards. The reflection hook's `no new entry` run carries `reflection.applied: false`, and a run that wrote carries `true`.
- [ ] **hook env passthrough (FR12, FR52):** the reflection hook runs with its inherited lock marker set → the fake provider observes that environment variable with the same value.
- [ ] **prune usage errors (FR60):** `prune --older-than x`, `--older-than -1`, `prune some-id`, an unknown flag and `AGENTCLI_RETENTION_DAYS=soon` → exit 2 (JSON: `sdk_status: usage_error`), and no run is removed.
- [ ] **prune qualification (FR60):** a home with old `done`, `failed`, `cancelled`, `timeout` and `lost` runs, a fresh run, a `running` run, a `queued` run, a terminal run with no `ended_at` or an unparseable one, a run directory with no `state.json`, and one with a malformed `state.json` → only the five old terminal runs are removed. Result: `removed: 5`, `kept: 3`, `skipped: 4`, and `bytes_freed` equal to the size of the removed directories.
- [ ] **prune held lock (FR60):** an old terminal run whose run lock another process holds → kept, counted as kept, directory intact.
- [ ] **prune re-check under the lock (FR60):** between the first look and taking the lock, a run turns `running`, or its `ended_at` becomes recent, or a conversation names it as the active turn → the run is kept.
- [ ] **prune active marker (FR60):** an old terminal run that some conversation's `active_run_id` names → kept, and a different old run in the same home is removed.
- [ ] **prune index tidy (FR62):** a session index whose runs were all removed is deleted; an index that still has a run keeps only the ids whose run directory exists.
- [ ] **prune leftovers (FR60):** a `runs/.prune-*` directory left by an interrupted pass is deleted by the next pass and is never listed as a run.
- [ ] **auto prune once a day (FR61):** `prune.stamp` one hour or 23 hours old → `prune --auto` does nothing and leaves the stamp. 25 hours old, in the future, or unreadable → it prunes and rewrites the stamp.
- [ ] **auto prune opt-out (FR61):** `AGENTCLI_NO_PRUNE=1 agentcli prune --auto` → exit 0, nothing printed, no run removed, no stamp written.
- [ ] **auto prune on an empty home (FR61):** the home directory does not exist → exit 0, nothing printed, the home is not created.
- [ ] **auto prune already running (FR61):** another process holds `prune.lock` → exit 0 quietly, no run removed, no stamp written.
- [ ] **auto prune dry run (FR61):** `prune --auto --dry-run --json` reports what would go (`ran: true`), removes nothing and writes no stamp. `--auto --json` on a gated day prints one object with `ran: false`.
- [ ] **auto prune time budget (FR61):** twelve old runs and a budget shorter than the time to handle them (`AGENTCLI_TEST_PRUNE_BUDGET_MS`, `AGENTCLI_TEST_PRUNE_STEP_MS`) → exit 0, silent, some runs removed and some left. A second pass the same day changes nothing; with the stamp 25 hours old, the next pass removes the rest.
- [ ] **conversation without recorded sources (FR63):** a conversation record with no `*_source` fields and a first turn whose `request.json` exists → `send` takes the sources from that request. With the request gone → `profile`.
- [ ] **session ids are file-safe (FR62):** a session id with a path separator, `..`, a leading dot, `_` or 300 characters → indexed in a file inside `index/sessions/`, never shared with another session or with the empty-session key.
- [ ] **concurrent index appends (FR62):** forty admissions to one session at once → forty intact lines.
- [ ] **mod notice size (FR47):** output of 20 KiB → the notice contains the first 8 KiB and the output path.

---

## Failure & Error Handling

- [ ] **provider missing (FR5):** no `codex` on PATH → exit 127, outcome `error`, `error_excerpt` names the missing binary, a telemetry record is written, `state.json` is `failed`.
- [ ] **model rejected (FR15, FR16):** fixture `error-400` with exit 1 → exit 1, outcome `error`, `error_excerpt` is the event's message (≤ 200 chars, no ANSI), stderr only informational.
- [ ] **stderr fallback (FR16):** non-zero exit, no error events, stderr ending in `fatal: boom` → `error_excerpt` = `fatal: boom`.
- [ ] **timeout with descendants (FR26, US3):** the fake spawns a grandchild sleeper and ignores SIGTERM; `--timeout 2` → exit 124 within 2 + 15 s, outcome `timeout`, no surviving descendant processes (checked by process group), output path still printed.
- [ ] **foreground interrupt (FR26):** SIGTERM to `agentcli` while the fake runs → the provider group is gone, outcome `cancelled`, exit 143.
- [ ] **foreground visible to other shells (FR26):** while a foreground run is in progress → `agentcli status <run_id>` from another shell reports `running`, and `wait` returns when it finishes.
- [ ] **worker crash → lost (FR23):** SIGKILL the worker of a running job → the next `agentcli status <run_id>` reports `lost`. `wait` exits 125. The provider group recorded in the state is killed. The conversation is no longer busy. One telemetry record with outcome `lost`.
- [ ] **queued is not lost during admission (FR23):** a `status` issued while a job is `queued` and less than 30 s old with its lock free → it stays `queued`, not `lost`. After 30 s without a lock holder → `lost`.
- [ ] **orphan provider does not keep the lock (FR23):** SIGKILL only the worker while the fake provider keeps running → the run lock is free immediately, `status` reports `lost`, and the provider group is killed.
- [ ] **cancel after provider exit (FR24):** the provider exits 0, then a `cancel` lands before the worker finalizes → the state is `done` with the provider's exit code.
- [ ] **cancel a foreground run (FR24, FR26):** `agentcli cancel <run_id>` against a foreground run → the foreground command exits 130, outcome `cancelled`.
- [ ] **wait returns recorded exit (FR7):** `wait` on a run that ended with provider exit 1 → exits 1. On a foreground run cancelled by a bare SIGTERM → exits 143.
- [ ] **failed admission is recorded (FR21):** a worker that never reaches `running` → admission exits 70 and a telemetry record with outcome `error` exists for that run id.
- [ ] **send with missing cwd (FR20):** the conversation's working directory was deleted → `send` exits 2. `send --cwd <existing dir>` proceeds in that directory for that turn only.
- [ ] **launcher after uninstall (FR43):** the launcher's target directory removed → running `~/.local/bin/agentcli version` prints that the plugin is not installed and exits 127.
- [ ] **lost with reused pgid (FR23):** worker SIGKILLed and the provider gone; the recorded provider pgid now belongs to an unrelated process with a different start time → reconciliation marks `lost` and does not signal the unrelated process.
- [ ] **state write failure (FR29):** `state.json` replacement fails at finalization (read-only run dir injected by the test) → the foreground exit code is the provider's, with a warning on stderr. A later `status` from another shell reconciles the run as `lost`.
- [ ] **cancel running (FR24):** a job running on a slow fake → `agentcli cancel <run_id>` exits 0. Within 5 s + margin the state is `cancelled` (provider group gone), `wait` exits 130, and the telemetry outcome is `cancelled`.
- [ ] **cancel queued (FR24):** a job forced to stay `queued` (worker start blocked by test hook) → `cancel` marks it `cancelled` itself; the worker, if it later starts, exits without running the provider.
- [ ] **cancel terminal (FR24):** `cancel` on a `done` run → exit 0, state unchanged.
- [ ] **worker never reaches running (FR21):** worker start made to stall past 10 s → admission exits 70, state `failed`, the conversation marker is cleared, and no orphan worker remains.
- [ ] **telemetry unwritable (FR29, FR30):** telemetry directory read-only → the run's exit code is the provider's, with one warning line on stderr. The output and state are intact.
- [ ] **telemetry lock held (FR30):** another process holds the telemetry lock for 10 s → the run finishes within the 5 s lock wait and skips the record with a warning; the exit code is unchanged.
- [ ] **unknown ids (FR7, FR32):** `status`, `wait`, `result`, `cancel`, `send` and `annotate` with a nonexistent id → exit 4.
- [ ] **unsupported capability (FR11):** a test provider declaring no review support → `agentcli review --provider testprov --base main` exits 2 naming the capability.
- [ ] **pruned ids are not found (FR60):** after a prune, `status`, `wait`, `result`, `cancel`, `annotate` (by run id and by output path) and `send <run id>` on a pruned run all exit 4, and `status --session-id S` no longer lists it.
- [ ] **run vanishes mid-read (FR60):** a `status`, `wait`, `result`, `cancel`, `annotate` or `send <run id>` whose run directory is removed after it first read the run (test gate `AGENTCLI_TEST_READ_GATE`) exits 4; `result` does not report success with no output.
- [ ] **auto prune failure is loud (FR61):** the `runs` directory is read-only so a removal fails → `prune --auto` exits 70 and prints the reason on stderr.
- [ ] **indexed run directory gone (FR62):** the index lists a run whose directory was deleted → `status --session-id S` skips it without a warning and lists the others.
- [ ] **CI stale publish (FR40):** the dist workflow run for commit A finishes after commit B landed → the HEAD check fails and `dist` is not updated by A.

---

## Idempotency / Re-run Safety

- [ ] **legacy import re-run (FR37, US7):** import the legacy file twice → the second run adds 0 records, and the total equals the legacy line count (1,000 on the owner's file).
- [ ] **legacy duplicate lines (FR37):** a legacy file containing two byte-identical lines → two distinct imported runs (different occurrence index), and the re-run still adds 0.
- [ ] **stats parity (FR35, success metric):** fresh home + imported legacy file → `agentcli stats --all --json` equals the retired analyzer's `--all` output on the same file for every key the analyzer emits.
- [ ] **link re-run (FR43):** `link` twice with the same build → the second is a no-op (file unchanged, mtime unchanged).
- [ ] **link no downgrade (FR43):** a launcher with `build_seq` 50, then `link` from a build with `build_seq` 40 → the launcher is unchanged. From 60 → updated.
- [ ] **link stale path (FR43):** the launcher names an install path that was deleted → `link` from any build rewrites it.
- [ ] **annotate replay (FR33):** the same annotation appended twice → the folded attrs are identical to a single application.
- [ ] **mod dedup across reload (FR47):** a job finishes, a notice is submitted, then `/reload-plugins` → no second notice for that run id.
- [ ] **mod notified-set bound (FR47):** 501 finished jobs notified → the persisted set holds exactly the 500 most recent ids.
- [ ] **dist reproducible (FR40):** `make dist` twice on the same commit → identical binaries (byte-equal) and identical `SHA256SUMS`.

---

## Performance & Limits

- [ ] **wrapper overhead (success metric):** 50 foreground runs against a no-op fake → p50 of (`agentcli` wall time − fake wall time) ≤ 50 ms; report p50/p95. *Target is an assumption under validation.*
- [ ] **hook budget (FR26, US3):** `--timeout 300` with a fake that never exits and ignores SIGTERM → the command returns in ≤ 315 s.
- [ ] **large prompt (FR1, FR21):** a 2 MiB prompt via stdin, foreground and background → delivered intact (byte-equal at the fake), no pipe deadlock.
- [ ] **large output (FR27):** a 5 MiB output file → `result` prints it fully; `output_bytes` is correct.
- [ ] **stderr tail bound (FR27):** the fake writes 10 MiB to stderr → `stderr.tail` is ≤ 64 KiB and holds the last bytes.
- [ ] **status polling cost is flat (FR62):** 200 run directories of other sessions, each with an unreadable `state.json`, plus one indexed run of session X → `status --session-id X` lists that run, exits 0 and prints no warning: the other directories were never opened.
- [ ] **telemetry volume (FR34, FR35):** a month file with 20,000 records → `runs --all` and `stats --all` complete and report all records. Record their wall time as a baseline.
- [ ] **zero runtime dependencies (success metric, US5):** in a minimal container with only `/bin/sh`, coreutils and the fake provider (no Go, Python, Node, jq) → the plugin's shim and binary run exec, review, send, background, wait, runs and stats successfully.

---

## Notes

- Lifecycle cases (timeout, cancel, lost, concurrent admissions) run on Linux in Docker and on the macOS CI runner.
- Real-provider cases (FR57–FR59) run on the owner's machine and consume provider usage. They are not part of CI.
- The mod cases require a Claude Code build with mods enabled. They run through the plugin test runner where possible and manually in a live session for the toast/turn behaviour.
- A live `prices refresh` against the real price list, and the comparison of `stats` cost with an independent exact recomputation over the owner's telemetry (success metric), run on the owner's machine. They are not part of CI.
- Fixtures are recorded from codex-cli 0.159.3 and sanitized (fictitious thread/session ids, neutral paths) before they enter the repository (FR44).
