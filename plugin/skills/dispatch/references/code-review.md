# Code review

`agentcli review` runs the provider's own review of a diff and returns
severity-grouped findings with file and line. It takes exactly one target and no
prompt: a prompt with a target is refused (exit 2), so the review cannot be
steered. For a focused look, use cross-check.

Pick the target from git state:

| Situation | Target |
|---|---|
| local changes not yet committed | `--uncommitted` (staged, unstaged, untracked) |
| a feature branch about to become an MR/PR | `--base <main-branch>` |
| one commit that just landed | `--commit <sha>` |

## Dispatch

```bash
agentcli review --scenario code-review --uncommitted --background --json
agentcli review --scenario code-review --base master --background --json
agentcli review --scenario code-review --commit <sha> --background --json
```

Then `wait` and `result` as in SKILL.md. The mod has no review tool; use the CLI.

## When not to use this

A GitLab MR or GitHub PR reviewed in the session belongs to the `cr` skill (tier
gates, bot-finding triage). Use this for a quick independent pass on local
changes. When `cr` runs it in parallel mode, `cr` owns target resolution and the
merge of findings: start the review, return its output, and do not re-enter the
`cr` pipeline.

## After the run

1. Findings arrive ordered by severity. Triage with the user: "N findings (X
   critical, Y high, Z medium). Walk through all, or only the critical ones?"
2. Never auto-apply fixes. Each finding is a decision for the user.
3. If you already reviewed these changes, name the overlap: what both found,
   what the other agent found that you missed, and what you found that it did not.
4. Record the count so `agentcli stats` can report it:

```bash
agentcli annotate <run_id> --attr-json 'review.findings={"total":3,"critical":0,"high":1,"medium":2}'
```

## Anti-patterns

- Reviewing an MR/PR description: the review reads the diff, not the prose. Use
  second-opinion for the description.
- Asking for something that contradicts review ("write tests for this"). That is
  delegation.
