# Verification Checklist: agent-cli-sdk (`agentcli`)

**Date**: 2026-10-04
**Status**: Draft

Unless stated otherwise:
- `AGENTCLI_HOME` points at a fresh temporary directory.
- A fake `codex` is first on PATH. It replays a named recorded event stream (sanitized fixtures: `exec-ok`, `review-ok`, `resume-ok`, `error-400`), writes the `-o` file, and exits with a scripted code.
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
- [ ] **stats (FR35, US7):** with mixed records → `agentcli stats --all --json` has every key `total, empty, span, by_source, by_source_status, instrumentation, reliability, outcomes, duration_ms, findings, review_findings_proxy, window_days`, plus `by_provider` and `usage_totals`.
- [ ] **version (FR10):** `agentcli version --json` → has `version`, `source_commit`, `build_seq` (integer > 0 for CI builds) and `platform`.
- [ ] **link fresh (FR43, US5):** no launcher present → `agentcli link`.
  - It creates `~/.local/bin/agentcli` (directory created if missing), which executes the resolved install's `bin/agentcli` and carries its `build_seq`.
  - `~/.local/bin/agentcli version` matches.
- [ ] **shim platform selection (FR41):** on linux/amd64 → `bin/agentcli version --json` reports `platform: linux/amd64`. The same holds on the macOS CI runner for darwin.
- [ ] **mod ask (FR46, FR47, US1):** in a session with the mod, Claude calls `ask {prompt}`.
  - It returns `{conversation_id, run_id}` at once.
  - Within one poll interval after the job ends, a toast appears and a turn is submitted whose text starts `agent-cli job <run_id>` and contains the outcome and the output inline (≤ 8 KiB).
- [ ] **mod send (FR46):** `send {conversation_id, prompt}` → a job for turn 2, with a notice on completion.
- [ ] **mod jobs (FR46):** `jobs {action: "list"}`, `{action: "status", run_id}`, `{action: "result", run_id}`, `{action: "cancel", run_id}` each return the CLI's JSON or content.
- [ ] **mod status line and command (FR48):** two running jobs → the status line shows 2. `/agent-cli-jobs` prints the session's recent jobs while Claude is mid-turn.
- [ ] **provider version (FR17):** fake `codex --version` prints `codex-cli 9.9.9` → the record's `provider_version` is `codex-cli 9.9.9`. A fake whose version flag exits 1 → `provider_version: null`, and the run is otherwise unaffected.
- [ ] **terminal ordering (FR22):** a test observer polls a job; at the first poll where `state.json` is terminal, `output.md` already has its final bytes and the conversation's marker is already cleared. The telemetry record appears after the terminal state, never before.
- [ ] **job survives its launcher (FR25):** start a job from a shell, then kill that shell's whole process group → the job still reaches `done`, and `wait` from a new shell exits 0.
- [ ] **namespaced attrs (FR36):** `--attr-json review.findings='{"total":2}'` → stored under the literal key `review.findings`, not nested as `review → findings`.
- [ ] **marketplace manifest (FR39):** `claude plugin validate` on the repository root passes. The manifest lists one plugin `agent-cli` with source `github`, `ref: dist` and no `sha`.
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
- [ ] **skill gate (FR55):** in a session without the dispatch skill loaded, each of `agentcli exec "q"`, `nohup agentcli exec "q"`, `FOO=1 agentcli exec "q"`, `/abs/path/agentcli exec "q"` and `codex exec "q"` triggers the precondition naming `agent-cli:dispatch`. `echo agentcli` does not.
- [ ] **old names gone (FR55, FR56):** a text search for the old skill name across the listed skills, rules, hook docs and eval files returns no stale reference. A text search for `codex exec`/`codex review` invocations across hook scripts, hook libraries, scripts, commands and skill scripts returns zero (success metric 1).
- [ ] **install check (FR59, US5):** after the marketplace install, a Bash tool `command -v agentcli` resolves inside the plugin. After a session start, a settings-hook probe resolves `~/.local/bin/agentcli`.

---

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
- [ ] **link without install record (FR43):** no installed-plugins record for `agent-cli@agent-cli-sdk` → `agentcli link` exits 4 with a message. `agentcli link --target /work/agentcli-shim` writes a launcher executing that path with the running binary's `build_seq`.
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
- Fixtures are recorded from codex-cli 0.159.3 and sanitized (fictitious thread/session ids, neutral paths) before they enter the repository (FR44).
