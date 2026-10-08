# Implementation frame

Shared decisions every contributor builds against. The requirements live in
`docs/PRD.md` (FR ids) and `docs/test-cases.md`. This file fixes what crosses
package boundaries so no slice invents its own version.

## Toolchain

- Go 1.27 (`golang:1.27.1-bookworm`), `CGO_ENABLED=0`, built with `-trimpath`.
- Dependencies: the standard library, plus `golang.org/x/sys/unix` where the
  standard library has no equivalent (process start time on darwin). Nothing else.
- All Go commands run through `make` (Docker). The host needs only Docker.
  `make test TESTFLAGS='-run TestX ./internal/foo/...'` narrows a run.
- Version metadata comes from ldflags into `internal/version`
  (`Version`, `SourceCommit`, `BuildSeq`). Defaults: `dev`, `unknown`, `0`.

## Package layout

| Package | Owns |
|---|---|
| `cmd/agentcli` | `main`: calls `cli.Main(os.Args, env) int` and exits with its result |
| `internal/cli` | argument parsing, command registry, exit codes, stdout/stderr contract |
| `internal/provider` | the `Provider` interface, capability types, provider registry |
| `internal/provider/codex` | the Codex adapter: plan building and event parsing |
| `internal/runner` | plan → spawn → stream-parse → finalize, for foreground and worker modes |
| `internal/store` | home resolution, run directories, conversations, state files, locks |
| `internal/telemetry` | record types, append, fold, `runs` export, `stats` |
| `internal/prices` | the price cache, price-list parsing, wanted models, run cost |
| `internal/profile` | built-in scenario profiles and precedence resolution |
| `internal/link` | launcher resolution and writing |
| `internal/version` | build metadata |
| `internal/testutil/fakecodex` | the fake provider binary used by tests (a `main` package) |
| `test/e2e` | black-box tests against the built `agentcli` binary |
| `tools/import-legacy` | the legacy telemetry import program (FR37) |
| `plugin/` | plugin content copied into `dist`: skill, mod, commands, hooks |

## Command registration

Each command lives in its own file in `internal/cli` (`cmd_exec.go`,
`cmd_review.go`, …) and registers itself from an `init()` into the package's
command table. No slice edits a shared dispatch switch.

## Exit codes

One file, `internal/cli/exitcodes.go`, holds the SDK codes from FR5:
`2` usage · `3` conversation busy · `4` not found · `5` wait timed out ·
`6` not resumable · `7` price list unavailable · `70` internal · `124` timeout · `125` lost ·
`127` provider missing · `130` cancelled by request. Signals map to `128+n`.
Provider exits pass through unchanged.

## Provider contract

```go
type Capabilities struct {
    Commands          []string // "exec", "review", "resume"
    ReviewTargets     []string // "base", "uncommitted", "commit"
    Sandboxes         []string // "read-only", "workspace-write"
    ReportsUsage      bool
    PreassignsSession bool
}

type Plan struct {
    Argv      []string          // argv[0] is the provider binary name
    Stdin     StdinSource       // StdinPrompt or StdinEmpty
    Dir       string
    EnvAdd    map[string]string
    EnvRemove []string
}

type Event struct {
    SessionID string  // non-empty when this event reveals the provider session
    Usage     *Usage  // non-nil on usage-bearing events
    ErrorMsg  string  // non-empty on error-bearing events
}

type Provider interface {
    Name() string
    Capabilities() Capabilities
    ReservedFlag(arg string) (reserved bool)
    BuildPlan(req Request) (Plan, error)
    ParseEvent(line []byte) (Event, bool) // false: unparseable line
    VersionArgs() []string
}

// Optional, like ModelReporter: a provider that can list the models it
// offers and names the price-list namespace of their entries.
type ModelCatalog interface {
    PriceNamespace() string                         // codex: "openai"
    CatalogModels(env []string) ([]string, error)  // codex: `codex debug models`, models[].slug
}

// Optional: a provider that can tell what a run has used so far, from its own
// session records. ok is false when there is nothing to report yet.
type LiveUsageReporter interface {
    LiveUsage(env []string, sessionID string, since time.Time) (u Usage, model string, ok bool, err error)
}
```

`Request` carries: command (`exec`/`review`/`resume`), prompt presence, cwd,
model, effort, sandbox, review target, provider session id, passthrough args,
output file path. Field names are the implementer's choice as long as the
semantics are these.

Codex specifics that are decided:
- Argv order exactly as FR13. The output file is always passed with `-o`.
- An `error` event or a `turn.failed` event provides `ErrorMsg`. When that
  message is itself a JSON document with `.error.message`, the inner message is
  used. `item.completed` events whose item type is `error` are not errors (they
  are warnings) and are ignored.
- Usage that is all zeros on a review becomes null.

## Persisted formats

All JSON, written with 0600 permissions under directories with 0700.
Atomic writes are a temp file in the same directory plus rename.

`runs/<run_id>/state.json`:

```json
{
  "run_id": "r-20261004T231500Z-a1b2c3d4",
  "conversation_id": "c-20261004T231500Z-9f8e7d6c",
  "turn": 1,
  "state": "queued|running|done|failed|cancelled|timeout|lost",
  "background": false,
  "admitted_at": "RFC3339 UTC",
  "started_at": "RFC3339 UTC or null",
  "ended_at": "RFC3339 UTC or null",
  "worker_pid": 0,
  "provider_pgid": 0,
  "provider_start_time": "opaque string or null",
  "exit_code": null,
  "outcome": null,
  "provider_exit": null,
  "sdk_status": null,
  "error_excerpt": null,
  "unparsed_events": 0,
  "output_path": "absolute path",
  "run_dir": "absolute path"
}
```

`provider_exit` (int or null) and `sdk_status` (string) are null until the run
is terminal and are written at every terminal transition: finish, abort, lost
and cancel-while-queued. Readers print what is recorded; they derive both from
`state` and `exit_code` only for a state file that has no `sdk_status`.

`runs/<run_id>/request.json`: the resolved request (command, provider,
scenario, model, effort, sandbox and their `_source`s, cwd, source, session id,
timeout, clean sentinel, material label, attrs, passthrough) plus the plan.

`conversations/<conversation_id>.json`:

```json
{
  "conversation_id": "c-…",
  "provider": "codex",
  "provider_session_id": null,
  "cwd": "absolute path",
  "defaults": {"scenario": "…", "model": "…", "effort": "…", "sandbox": "…",
               "model_source": "flag|profile|provider-default", "effort_source": "…", "sandbox_source": "…"},
  "turns": ["r-…"],
  "active_run_id": null,
  "created_at": "RFC3339 UTC",
  "updated_at": "RFC3339 UTC"
}
```

`status` (`busy`/`idle`) and `resumable` are derived on read, not stored.

A cancel request is the presence of `runs/<run_id>/cancel.request`.

Retention files: `index/sessions/<session id>` (one run id per line, appended at
admission; an empty session id uses the key `_`) and `prune.stamp` (the time of
the last `prune --auto`, written under `prune.lock`). Both live under the home.
Index appends are one O_APPEND write under `index/lock`, which prune also takes
when it rewrites or deletes an index file. A session id that is not file-safe
(a path separator, a leading dot, longer than 128, or a literal `_`) is keyed
`~<first 16 hex of sha256>`. A pruned run is first renamed to
`runs/.prune-<id>`, then deleted; readers ignore dot-prefixed run directories.
`prune --json` prints `sdk_status, exit_code, ran, dry_run, older_than_days,
removed, kept, skipped, failed, bytes_freed, stopped_early`.

`runs/<run_id>/worker.log` is the stderr of a job's worker (0600), bounded to
its last 64 KiB like `stderr.tail`; runner warnings raised inside a job land
there.

Lock files: `runs/<run_id>/lock`, `conversations/<conversation_id>.lock`,
`telemetry/.lock`, and the launcher's lock next to the launcher. All use
`flock(2)` exclusive locks; descriptors are close-on-exec.

Telemetry records: exactly the FR31 fields for `run`, and
`{v, kind, run_id, ts, attrs}` for `annotation`, one JSON object per line.

`prices.json` (FR71), written atomically under `prices.lock` by
`prices refresh`; prices are decimal strings in US dollars per million tokens,
already resolved (an omitted cache-read or cache-write price is the input
price):

```json
{
  "v": 1,
  "source": "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json",
  "etag": "\"…\"",
  "fetched_at": "RFC3339 UTC",
  "checked_at": "RFC3339 UTC",
  "unit": "usd_per_1m_tokens",
  "models": {
    "codex": {
      "gpt-6.1-sol": {"input": "2", "cached_input": "0.1", "cache_write": "2.5", "output": "10"}
    }
  },
  "unpriced": {"codex": ["codex-auto-review", "gpt-reserve"]}
}
```

Decimal strings are the shortest exact form: no exponent, no trailing zeros
after the point, and no point for whole numbers. `prices.lock` uses `flock(2)`
like the other locks.

## Test harness

- `test/e2e` builds `agentcli` and `fakecodex` once in `TestMain` into a temp
  directory, puts the fake first on `PATH` as `codex`, and gives each test its
  own `AGENTCLI_HOME`.
- `fakecodex` reads its behaviour from environment variables:
  - `FAKECODEX_FIXTURE`: path of a JSONL fixture streamed to stdout.
  - `FAKECODEX_OUTPUT`: text written to the `-o` file; when unset, the text of the
    fixture's last `agent_message` item.
  - `FAKECODEX_EXIT`: exit code.
  - `FAKECODEX_STDERR`: text written to stderr.
  - `FAKECODEX_SLEEP_MS`: delay before exiting.
  - `FAKECODEX_IGNORE_TERM=1`: ignore SIGTERM.
  - `FAKECODEX_SPAWN_CHILD=1`: start a sleeping grandchild in the same group.
  - `FAKECODEX_RECORD`: a path to write `{argv, stdin, env, cwd}` as JSON.
  - `FAKECODEX_VERSION`: the output of `--version`.
  - `FAKECODEX_PIPE_HOLDER=<pidfile>`: start a `setsid` grandchild that keeps
    stdout/stderr open and write its pid to the file.
  - `FAKECODEX_CHILD_IGNORE_TERM=1`: the spawned grandchild ignores SIGTERM.
  - `FAKECODEX_MODELS`: path of a JSON file printed as-is by `codex debug models`
    (exit 0). When unset, `debug models` prints an error and exits 1.
- Every e2e sandbox sets `AGENTCLI_PRICES_URL=off` unless the test sets it to the
  URL of an `httptest` server on 127.0.0.1 that serves a fixture price list
  (`testdata/prices/`).
- Live-usage tests set `CODEX_HOME` to a temp directory and write a Codex session
  file there (`sessions/<y>/<m>/<d>/rollout-<time>-<thread id>.jsonl`) holding
  `turn_context` and `token_count` lines; the fake provider writes none itself.
- `AGENTCLI_TEST_PRICES_TIMEOUT_MS` and `AGENTCLI_TEST_PRICES_MAX_BYTES` shorten
  the refresh's 20-second timeout and 32 MiB body limit for tests.
- `AGENTCLI_TEST_SHUTDOWN_GRACE_MS` and `AGENTCLI_TEST_SHUTDOWN_DRAIN_MS`
  shorten the termination grace and the pipe drain for tests. Production
  defaults are 5 seconds each.
- Fixtures in `testdata/codex/` are recorded Codex streams with fictitious ids.
  Never put real ids, emails or home paths in the repository.

## Output contract details

- `sdk_status` takes one value per SDK outcome, mirroring the exit codes:
  `ok` (the SDK did its part; the provider's own exit may still be non-zero),
  `usage_error`, `busy`, `not_found`, `not_resumable`, `wait_timeout`,
  `internal_error`, `timeout`, `lost`, `provider_missing`, `cancelled`,
  `prices_unavailable`.
  `provider_exit` is null when the provider never ran.
- With `--json`, every exit path prints exactly one JSON object on stdout,
  usage errors included: `{"sdk_status": "...", "exit_code": N, "error": "..."}`
  plus whatever ids exist at that point.
- `--dry-run` prints `{command, provider, run_id, argv, stdin, cwd, env_add,
  env_remove}`; `stdin` is `prompt` or `empty`, and `argv` names the real
  future output path.
- `--skip-git-repo-check` sits after `-o <path>` and before passthrough flags.
  A work tree is detected by walking up from the cwd for a `.git` entry.
- An error event sets `error_excerpt` even when the provider exits 0.
- Flags may be interspersed with the positional prompt; everything after the
  first literal `--` is passthrough.
- `ReservedFlag` is called with each passthrough token alone and with each
  token joined to its successor by one space, so `-c model=x` arrives whole.
- A conversation's `defaults` hold the resolved model, effort and sandbox
  (profile applied), so later turns reuse them unless a flag overrides.
- In a run record an absent value is `null`, never an empty string
  (`session_id`, `provider_session_id`, `model`, …). `output_bytes` is `0` when
  the output file is missing. `ts` is the run's start at second resolution.
- The provider version probe runs beside the provider, is bounded at 5 seconds
  and keeps the first non-empty stdout line.
- Fold: an annotation that precedes its run record in append order (a running
  job annotated before it finishes) is applied once that record is read; an
  annotation whose run never appears creates nothing. Duplicate run records
  for one `run_id` keep the first.
- `annotate` exits 70 when the lock or the write fails: the annotation is the
  command's whole job, unlike a run's telemetry.
- Imported legacy records keep fields that have no run-record name (such as
  `status`) as given; native runs derive `status` from `outcome` on read.
- A conversation's active-turn marker is cleared only by the run it names.
- Termination: after SIGTERM to the provider group, SIGKILL follows as soon as
  the group leader exits or the 5-second grace ends, whichever comes first.
  On timeout or cancellation `provider_exit` is the observed wait status
  (for example 143 or 137); `sdk_status` is `timeout` or `cancelled`
  (`cancelled` for both 130 and 128+n). `--timeout` takes a positive integer
  of seconds.
- Jobs: the worker is the hidden `agentcli _worker <run_id>`, started with
  `setsid` and null stdio. `result` on a non-terminal run exits 3 and names
  `wait`. `status --json` prints `{"runs": [...]}`, `conversations --json`
  prints `{"conversations": [...]}`; a run object's `exit_code` is the run's
  recorded exit. `status` lists newest first by `admitted_at`, then run id.
- `wait` returns after finalization: the run is terminal and its run lock is
  released, so the conversation marker and the telemetry record are written.
  It waits at most 10 seconds past the terminal state for the lock; if it is
  still held it prints one warning on stderr and returns anyway (seam:
  `AGENTCLI_TEST_FINALIZE_WAIT_MS`). `--timeout` covers the whole wait and
  exits 5 when it passes during finalization.
- Test seams for jobs: `AGENTCLI_TEST_ADMISSION_WAIT_MS` (admission wait,
  default 10 seconds), `AGENTCLI_TEST_WORKER_STALL_MS` (the worker sleeps
  before taking the run lock) and `AGENTCLI_TEST_QUEUED_GRACE_MS` (how long a
  queued run may go without a lock holder before readers count it lost,
  default 30 seconds).
- Lost: every reader of a non-terminal run (`status`, `wait`, `cancel`,
  `result`, `send`) probes the run lock without blocking. A free lock with the
  state `running`, or with `queued` for longer than the grace, settles the
  run under the run lock itself, after re-reading the state, so racing readers
  settle it once; a reader that finds the lock held leaves the run alone. The
  provider group is killed only when its leader still has the recorded start
  time. The settled run is `lost`, exit 125, `sdk_status` `lost`,
  `provider_exit` null, and finishes in the usual order.
- Cancel: `agentcli cancel <run_id> [--json]` writes `cancel.request`, then a
  queued run no worker holds is marked `cancelled` (exit 130) by `cancel`
  itself, and a running run gets SIGTERM sent to its recorded worker pid. A
  queued run whose lock a worker holds is awaited until it is running (bounded
  by the admission wait). `cancel` exits 0 whichever way the request lands,
  4 for an unknown id, and prints the run like `status`; on a terminal run it
  writes nothing. A worker re-reads the state once it holds the run lock: a
  terminal state, or a cancel request on a queued run, ends the worker without
  spawning the provider and without touching the state. An admission whose
  worker left for that reason ends the run `cancelled`, not `failed`.

## Rules for every slice

- Test-first: one failing test, minimal code to pass it, refactor on green.
  Tests exercise the CLI or a package's exported surface, never internals.
- Run `make test` (or a narrowed `TESTFLAGS`) and paste the output in the report.
- No git commits, no pushes, no network access other than module downloads, and
  never invoke a real `codex` binary. A test server on 127.0.0.1 inside the test
  process is not network access.
- Code comments state the behaviour or invariant. They never cite requirement
  or test-case ids (`FR…`); those belong in commit messages.
- Never stop, kill or remove a container, process or file you did not create.
  Every `make` container carries the label `agentcli.dev=1`; a hung test is
  stopped with `docker ps -q --filter label=agentcli.dev=1 | xargs -r docker kill`,
  never with a host-wide `docker ps -q`.
- A choice that would change a documented contract (CLI, exit codes, persisted
  formats, telemetry fields) is not made inside a slice: report it back.
