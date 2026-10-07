# Cross-check implementation

A post-implementation sanity pass: the other agent reads code just written (by
Claude or the user) and surfaces bugs, missed edge cases and brittle
assumptions. It targets named files or a feature as a whole, independent of git
state. If the user has a diff in mind, use code-review instead.

| | code-review | cross-check |
|---|---|---|
| Input | a git diff | an explicit file list |
| Scope | what changed | what exists, including untouched files that frame the change |
| Use when | pre-merge | after implementation, no diff context |

## Dispatch

```bash
agentcli exec --scenario cross-check --cwd <repo-root> --background --json - <<'PROMPT'
...template below...
PROMPT
```

Mod: `mcp__agentcli__ask` with `scenario: "cross-check"` and `cwd` set to the
repository, so the file reads resolve.

## Prompt template

```
## What this code does
[one paragraph: the feature or module and its responsibility]

## Files to inspect
[absolute paths; the reviewer reads them itself]
- /abs/path/to/file1
- /abs/path/to/file2

## Where it sits
[what calls it, what it calls]

## What I'm worried about
[a specific worry, or "generic sanity pass"]

## What I need from you
Read the files above. Find:
1. Logic bugs: wrong condition, off-by-one, wrong branch
2. Unhandled edge cases: empty inputs, nulls, concurrency, error paths
3. Brittle assumptions: implicit invariants that could break
4. Resource issues: leaks, unbounded growth, missing cleanup
5. Anything copy-pasted but not adapted

Report findings ordered by severity with file:line. Skip style.
```

## After the run

- Group the findings and walk through them: "N items, A critical, B medium."
- Decide each one: accept and fix, the reviewer misunderstood (say why), or valid
  but not now (backlog).
- Mostly "misunderstood" means the prompt context was thin. Improve it, then
  `send` the correction to the same conversation.

## Anti-patterns

- Pasting whole files into the prompt: the reviewer reads paths itself, and
  pasting costs input tokens and skews attention.
- Cross-checking code a delegation just produced on the same model: same blind
  spots. Pass a different `--model`, or let Claude check it.
