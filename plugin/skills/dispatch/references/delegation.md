# Delegation

Offload a well-scoped, mechanical task to the other agent while Claude keeps
orchestrating: boilerplate, a large refactor with a clear pattern, bulk renames,
generated test scaffolds, schema-driven code. This is the only scenario whose
profile writes to the workspace.

## Route elsewhere when

- "Figure out what is wrong": diagnose, or cross-check.
- "Decide between A and B": second-opinion.
- "Review what I wrote": code-review or cross-check.
- "Explain how X works": answer directly; no dispatch.

## Checklist before dispatching

If any line is false, tighten the scope or do the work inline.

- [ ] Scope is bounded ("rename `X` to `Y` across `src/**/*.ts`", not "improve auth").
- [ ] Success is objective: tests pass, file exists with a shape, no occurrence of the old pattern.
- [ ] The working tree is clean, or the user accepts the diff risk. The agent
      writes into the workspace; uncommitted changes make review hard.
- [ ] No production-critical files in the write surface (infra config, `.env*`,
      secrets, deploy YAML). If there are, ask the user first.
- [ ] The spec is locked. Do not delegate while the user is still iterating on the design.

## Dispatch

Pin the repository with `--cwd`; never run it from a directory holding several
projects. While the job runs it may be writing to the tree, so make no
conflicting edits in the same repository until it finishes.

```bash
agentcli exec --scenario delegation --cwd "$REPO_ROOT" --background --json - <<'PROMPT'
## Task
[crisp description]

## Files in scope
[explicit glob or file list; stay within these]

## Files OFF-LIMITS
[sensitive paths not to touch]

## Pattern to follow
[reference implementation: "make X look like Y at file:line"]

## Success criteria
[objective check]

## When stuck
Stop and report back. Do not improvise; do not expand scope.
PROMPT
```

Mod: `mcp__agent-cli__ask` with `scenario: "delegation"` and `cwd`. A job running
far past its peers means the task was not mechanical: cancel it
(`mcp__agent-cli__jobs` with `action: "cancel"`, or `agentcli cancel <run_id>`)
and re-scope.

## After the run

1. Read the output: it usually summarizes what changed. A summary describes
   intent, not result.
2. Run `git status` and `git diff` to see the actual changes.
3. Run the success check (tests, a grep for the old pattern).
4. Show the diff to the user before committing, even when the agent says done.
   The user owns the decision to keep the changes.

## When it goes wrong

- Edits outside the scope: `git checkout` those files; do not repair them in
  Claude. Re-dispatch with a tighter scope.
- Tests fail afterwards: read the diff and judge whether the fault is the
  refactor or stale tests before asking the agent to fix them.
- Stopped midway: read the output for the reason (usually the scope was wider
  than it looked), then `send` the remainder to the same conversation.

## Anti-patterns

- "Do X and decide Y": the agent does not know the project's unstated conventions.
- Subjective success criteria ("cleaner", "faster").
- Cross-checking the result on the same model; see cross-check.
