# Expert persona

Frame the other agent as a senior practitioner in a specific domain before asking.
"You are a senior X with N years in Y" shifts the answer toward domain-idiomatic
substance and away from generic advice. This scenario's profile runs at the
highest default effort because the framing makes the extra thinking productive.

## Lock the persona first

Ask the user if an axis is ambiguous: the persona is the load-bearing part.

| Axis | Examples |
|---|---|
| Role | senior backend engineer, staff SRE, principal data scientist, security architect, DBA |
| Domain | distributed systems, payments infra, healthtech compliance, ML inference at scale |
| Stack | NestJS + MongoDB + Kafka, AWS ECS + EFS, Postgres at 10M+ rows, Rust + tokio |
| Depth signal | "10+ years", "shipped X to production", "operated Y at Z scale" |
| Stance | "favors operational simplicity over cleverness", "skeptical of new frameworks" |

Three or four axes at most. Past that the persona reads as contrived and the
answer snaps back to generic.

## Dispatch

```bash
agentcli exec --scenario expert-persona --background --json - <<'PROMPT'
...template below...
PROMPT
```

Mod: `mcp__agentcli__ask` with `scenario: "expert-persona"`.

## Prompt template

```
You are a senior [role] with [N]+ years of hands-on experience in [domain].
Your background: [stack specifics and scale signals, one or two lines].
Your stance: [what you value, what you push back on].

Draw on patterns you have seen in production. Cite specific failure modes,
tradeoffs and what you would actually do, not textbook answers. If the question
is ill-posed or missing context, say so before answering.

## The question
[the user's question, in their own framing]

## Context I have
[constraints, prior decisions, stack details]

## What I need
[opinion, design sketch, risk inventory, recommendation]
```

## After the run

- Attribute with the persona: "Codex (as a senior [domain] engineer) says…".
- Relay its pushback unsanitized. "You are asking the wrong question" is often
  the most valuable part.
- A generic answer despite the framing means the axes were too vague. Re-prompt
  with more stack, scale and stance.
- For a genuinely hard, long-horizon problem where the profile feels
  capability-bound, raise `--effort` or pick a stronger `--model`, and still
  cross-check judgment calls against Claude.

## Anti-patterns

- Six or more persona axes: it reads like a job posting.
- Asking the persona to write code. Expert framing is for judgment; use
  delegation for code.
- Using it for a question the user could answer faster than the run takes.
