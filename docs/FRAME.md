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
`6` not resumable · `70` internal · `124` timeout · `125` lost ·
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
  "error_excerpt": null,
  "unparsed_events": 0,
  "output_path": "absolute path",
  "run_dir": "absolute path"
}
```

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
  "defaults": {"scenario": "…", "model": "…", "effort": "…", "sandbox": "…"},
  "turns": ["r-…"],
  "active_run_id": null,
  "created_at": "RFC3339 UTC",
  "updated_at": "RFC3339 UTC"
}
```

`status` (`busy`/`idle`) and `resumable` are derived on read, not stored.

Lock files: `runs/<run_id>/lock`, `conversations/<conversation_id>.lock`,
`telemetry/.lock`, and the launcher's lock next to the launcher. All use
`flock(2)` exclusive locks; descriptors are close-on-exec.

Telemetry records: exactly the FR31 fields for `run`, and
`{v, kind, run_id, ts, attrs}` for `annotation`, one JSON object per line.

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
- Fixtures in `testdata/codex/` are recorded Codex streams with fictitious ids.
  Never put real ids, emails or home paths in the repository.

## Rules for every slice

- Test-first: one failing test, minimal code to pass it, refactor on green.
  Tests exercise the CLI or a package's exported surface, never internals.
- Run `make test` (or a narrowed `TESTFLAGS`) and paste the output in the report.
- No git commits, no pushes, no network access other than module downloads, and
  never invoke a real `codex` binary.
- A choice that would change a documented contract (CLI, exit codes, persisted
  formats, telemetry fields) is not made inside a slice: report it back.
