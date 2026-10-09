# Second opinion

The other agent evaluates a decision, plan, design choice or proposal that Claude
has already drafted or analyzed. Its output is a critique, not code.

## Dispatch

```bash
agentcli exec --scenario second-opinion --background --json - <<'PROMPT'
...template below...
PROMPT
```

Mod: `mcp__agentcli__ask` with `scenario: "second-opinion"` and the same prompt.

## Prompt template

```
## Context
[what has been decided or is being considered]

## The decision
[the specific choice, framed as crisp options if possible]

## My current lean
[Claude's recommendation, honestly stated so the reviewer can push back]

## Files / artifacts referenced
[absolute paths, or the pasted snippets that matter]

## What I need from you
Give me your independent take:
1. Do you agree with the recommendation, and why or why not?
2. What is the strongest counter-argument I am not weighting enough?
3. Any blind spot, edge case or risk not yet named?
4. If you would choose differently, what and why?

Be direct. Disagreement is the point; consensus theater is not useful here.
```

## After the run

- Open with the verdict: "Codex agrees" or "Codex disagrees on [X]".
- On disagreement, do not capitulate automatically. Weigh the reasoning, then
  update the recommendation, rebut it, or put both takes in front of the user.
- Pressing on one point is a `send` to the same conversation, not a new dispatch.

## Anti-patterns

- Asking it to "review the conversation": it has none. Extract the decision,
  artifacts and lean into the prompt.
- Dispatching on trivial choices (naming, formatting). Reserve it for decisions
  where being wrong is expensive.
- Dispatching when Claude is confident and the user already agrees. The value is
  in real uncertainty or real stakes.
