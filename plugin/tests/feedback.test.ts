import { expect, test } from 'claude-code/testing'
import { POLL_MS, SESSION, job, listing, ok, step, world } from './kit'
import { compactCount, heartbeatLine } from '../hooks/subagent.js'

const START = { surface: 'terminal', isInteractive: true, cwd: '/work' } as const

function progress(next: number, entries: { kind: string; text: string }[]) {
  return ok(JSON.stringify({ run_id: 'run-1', state: 'running', next, entries: entries.map((e, i) => ({ seq: next - entries.length + i, at: 't', ...e })) }))
}

function finishedRun(over: Record<string, unknown> = {}) {
  return ok(JSON.stringify({ run_id: 'run-1', conversation_id: 'conv-1', state: 'done', outcome: 'ok', output_path: '/o', ...over }))
}

const WAIT_TIMEOUT = { exitCode: 5, stdout: JSON.stringify({ sdk_status: 'wait_timeout', exit_code: 5 }), stderr: '' }

function thinkingOf(chunks: any[]) {
  return chunks.filter((c) => c.kind === 'thinking').map((c) => c.text.trim())
}

test('what the run does streams into the agent row as thinking, then the answer', async ($, on) => {
  const w = world(on)
  let waits = 0
  const reads: string[] = []
  w.respond = (argv) => {
    if (argv[1] === 'exec') return ok(JSON.stringify({ conversation_id: 'conv-1', run_id: 'run-1' }))
    if (argv[1] === 'wait') return ++waits < 2 ? WAIT_TIMEOUT : finishedRun()
    if (argv[1] === 'progress') {
      reads.push(argv[4]!)
      return reads.length === 1 ? progress(2, [{ kind: 'message', text: 'I will read auth.go' }, { kind: 'command', text: 'rg -n token auth.go' }]) : progress(2, [])
    }
    if (argv[1] === 'status') return finishedRun()
    return ok('Codex: the token is never checked.')
  }
  w.agents.push({ id: 'ag-1', type: 'agent-cli:second-opinion' })
  w.messages['ag-1'] = [{ role: 'user', text: 'Is auth.go safe?' }]
  await $.session.start(START)

  const out = await step($, 'ag-1')

  expect(thinkingOf(out.chunks)).toEqual(['agentcli · second-opinion', 'I will read auth.go', '$ rg -n token auth.go'])
  // Each read starts where the last one ended.
  expect(reads).toEqual(['0', '2'])
  expect(out.chunks.filter((c) => c.kind === 'thinking').every((c) => c.index === 0)).toBe(true)
  expect(out.chunks.find((c) => c.kind === 'text')).toMatchObject({ index: 1 })
  expect(out.text).toContain('Codex: the token is never checked.')
})

test('a quiet run shows a heartbeat with the time it has been working', () => {
  expect(heartbeatLine(5000)).toBe('still working · 5s')
  expect(heartbeatLine(95000)).toBe('still working · 1m 35s')
  expect(compactCount(1408204)).toBe('1.4M')
  expect(compactCount(8900)).toBe('8.9k')
  expect(compactCount(999)).toBe('999')
})

test('a run another caller flagged is shown once as a hidden agent of the session', async ($, on) => {
  const w = world(on)
  const hookRun = job({ run_id: 'run-h', state: 'running', source: 'hook-post-commit', scenario: 'code-review', background: false, agent_feedback: true })
  w.respond = () =>
    listing(
      hookRun,
      job({ run_id: 'run-own', state: 'running', source: 'agent', agent_feedback: true }),
      job({ run_id: 'run-done', state: 'done', outcome: 'ok', source: 'hook-stop', agent_feedback: true }),
      job({ run_id: 'run-plain', state: 'running', source: 'cli' }),
    )
  await $.session.start(START)

  await w.clock.advance(POLL_MS * 2)

  expect(w.spawns.length).toBe(1)
  // A plugin's spawn reaches the engine as a background Agent call.
  expect(w.spawns[0]).toMatchObject({ subagent_type: 'agent-cli:run', description: 'code-review · hook-post-commit', prompt: 'agentcli run run-h', run_in_background: true })
  expect(w.store.get('agent-attached')).toEqual(['run-h'])
})

test('the hidden agent follows the run it was given and starts none', async ($, on) => {
  const w = world(on)
  w.respond = (argv) => {
    if (argv[1] === 'wait') return finishedRun({ run_id: 'run-h' })
    if (argv[1] === 'progress') return progress(1, [{ kind: 'command', text: 'git diff HEAD~1' }])
    if (argv[1] === 'status') return finishedRun({ run_id: 'run-h', scenario: 'code-review', source: 'hook-post-commit', outcome: 'findings' })
    throw new Error('unexpected agentcli call: ' + argv.join(' '))
  }
  w.agents.push({ id: 'ag-h', type: 'agent-cli:run' })
  w.messages['ag-h'] = [{ role: 'user', text: 'agentcli run run-h' }]
  await $.session.start(START)

  const out = await step($, 'ag-h')

  expect(w.runs.map((r) => r.argv[1])).toEqual(['wait', 'progress', 'status'])
  expect(w.runs[0]!.argv.slice(2, 3)).toEqual(['run-h'])
  expect(thinkingOf(out.chunks)).toContain('$ git diff HEAD~1')
  expect(out.text).toBe('agentcli run run-h (code-review, from hook-post-commit) finished: findings')
  expect(w.modelSteps).toEqual([])
})

test("the hidden agent's completion notice is never handed to Claude", async ($, on) => {
  const w = world(on)
  w.agents.push({ id: 'ag-h', type: 'agent-cli:run' }, { id: 'ag-1', type: 'agent-cli:adhoc' })
  await $.session.start(START)
  const notice = (id: string) =>
    `<task-notification>\n<task-id>${id}</task-id>\n<status>completed</status>\n<result>agentcli run run-h (code-review, from hook-post-commit) finished: findings</result>\n</task-notification>`

  const hidden: any = await $.prompt.submit({ text: notice('ag-h'), origin: { kind: 'task-notification' }, wait: false } as any)
  const own: any = await $.prompt.submit({ text: notice('ag-1'), origin: { kind: 'task-notification' }, wait: false } as any)

  expect(hidden.drop).toContain('finished: findings')
  expect(w.submits).toEqual([notice('ag-1')])
  expect(own.text).toBe(notice('ag-1'))
})

test('the hidden agent type is never offered to the model, gate open or not', async ($, on) => {
  const w = world(on)
  await $.session.start(START)
  await $.skill.prompt({ skill: 'agent-cli:dispatch', text: '' })

  const offered = await $.agent.offer({ agent: 'agent-cli:run', description: '', source: 'plugin', provider: { plugin: 'agent-cli', tier: 'user' } } as any)

  expect(offered).toEqual({ isOffered: false })
  expect(w.agentTypes.find((t) => t.name === 'run')).toMatchObject({ background: true })
})

test("the status line adds the session's Codex tokens, read again when a run ends", async ($, on) => {
  const w = world(on)
  let runs: any[] = [job({ run_id: 'a', state: 'done', outcome: 'ok', source: 'hook-stop' }), job({ run_id: 'b', state: 'running' })]
  let tokens = { input_tokens: 1408204, output_tokens: 7156, runs_with_usage: 1 }
  w.respond = (argv) => {
    if (argv[1] === 'stats') return ok(JSON.stringify({ usage_totals: tokens }))
    return listing(...runs)
  }
  await $.session.start(START)

  await w.clock.advance(POLL_MS)
  expect(w.statuses.at(-1)).toBe('1 job running · Codex 1.4M in · 7.2k out')
  const stats = w.runs.find((r) => r.argv[1] === 'stats')!
  expect(stats.argv.slice(1)).toEqual(['stats', '--session-id', SESSION, '--all', '--json'])

  // Read once more on the next poll, in case the run's record landed late;
  // then not again until another run ends.
  await w.clock.advance(POLL_MS)
  expect(w.runs.filter((r) => r.argv[1] === 'stats').length).toBe(2)
  await w.clock.advance(POLL_MS)
  expect(w.runs.filter((r) => r.argv[1] === 'stats').length).toBe(2)

  runs = [job({ run_id: 'a', state: 'done', outcome: 'ok', source: 'hook-stop' }), job({ run_id: 'b', state: 'done', outcome: 'ok' })]
  tokens = { input_tokens: 2500000, output_tokens: 9000, runs_with_usage: 2 }
  w.respond = (argv) => {
    if (argv[1] === 'stats') return ok(JSON.stringify({ usage_totals: tokens }))
    if (argv[1] === 'result') return ok('')
    return listing(...runs)
  }
  await w.clock.advance(POLL_MS)
  expect(w.runs.filter((r) => r.argv[1] === 'stats').length).toBe(3)
  expect(w.statuses.at(-1)).toBe('Codex 2.5M in · 9k out')
})

test('a refused spawn is retried on a later poll and logged once', async ($, on) => {
  const w = world(on)
  let refusals = 2
  w.spawnAnswer = () => (refusals-- > 0 ? { deny: 'background agents are disabled' } : { model: 'claude-haiku-4-5', agentId: 'ag-late' })
  w.respond = () => listing(job({ run_id: 'run-h', state: 'running', source: 'hook-stop', background: false, agent_feedback: true }))
  await $.session.start(START)

  await w.clock.advance(POLL_MS * 4)

  expect(w.spawns.length).toBe(3)
  expect(w.logs.filter((l) => l.includes('could not show run run-h')).length).toBe(1)
  expect(w.store.get('agent-attached')).toEqual(['run-h'])
})

test('a token count that cannot be read is read again on the next poll', async ($, on) => {
  const w = world(on)
  let fails = true
  w.respond = (argv) => {
    if (argv[1] === 'stats') return fails ? { exitCode: 70, stdout: '', stderr: 'agentcli: telemetry locked' } : ok(JSON.stringify({ usage_totals: { input_tokens: 2000, output_tokens: 10, runs_with_usage: 1 } }))
    if (argv[1] === 'result') return ok('')
    return listing(job({ run_id: 'a', state: 'done', outcome: 'ok', source: 'hook-stop' }))
  }
  await $.session.start(START)

  await w.clock.advance(POLL_MS)
  fails = false
  await w.clock.advance(POLL_MS)

  expect(w.statuses.at(-1)).toBe('Codex 2k in · 10 out')
  expect(w.logs.filter((l) => l.includes('token count')).length).toBe(1)
})

test('a progress read that fails leaves the run followed to its answer', async ($, on) => {
  const w = world(on)
  let waits = 0
  w.respond = (argv) => {
    if (argv[1] === 'exec') return ok(JSON.stringify({ conversation_id: 'conv-1', run_id: 'run-1' }))
    if (argv[1] === 'wait') return ++waits < 3 ? WAIT_TIMEOUT : finishedRun()
    if (argv[1] === 'progress') throw new Error('progress unavailable')
    if (argv[1] === 'status') return finishedRun()
    return ok('Codex answered anyway.')
  }
  w.agents.push({ id: 'ag-1', type: 'agent-cli:adhoc' })
  w.messages['ag-1'] = [{ role: 'user', text: 'q' }]
  await $.session.start(START)

  const out = await step($, 'ag-1')

  expect(waits).toBe(3)
  expect(out.text).toContain('Codex answered anyway.')
  expect(out.text).not.toContain('hand-off to the agent failed')
  expect(w.runs.some((r) => r.argv[1] === 'cancel')).toBe(false)
  expect(w.logs.filter((l) => l.includes('could not read the progress of run run-1')).length).toBe(1)
})
