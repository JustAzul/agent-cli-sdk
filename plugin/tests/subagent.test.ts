import { expect, test } from 'claude-code/testing'
import { SESSION, isShim, ok, step, world } from './kit'

const START = { surface: 'terminal', isInteractive: true, cwd: '/work' } as const
const HANDOFF_FAILED = 'agent-cli: the hand-off to the agent failed, so this is not its answer.'
const TYPES = ['adhoc', 'code-review', 'cross-check', 'delegation', 'expert-persona', 'second-opinion']
const JOB_FLAGS = ['--background', '--json', '--source', 'agent', '--session-id', SESSION]

function admitted(runId = 'run-1', conversationId = 'conv-1') {
  return ok(JSON.stringify({ conversation_id: conversationId, run_id: runId, state: 'running', sdk_status: 'ok', exit_code: 0 }))
}

function finished(over: Record<string, unknown> = {}) {
  return ok(JSON.stringify({
    run_id: 'run-1', conversation_id: 'conv-1', state: 'done', outcome: 'ok', error_excerpt: null,
    output_path: '/state/runs/run-1/output.md', ...over,
  }))
}

// codex answers every agentcli call of one successful turn.
function codex(output: string, over: Record<string, unknown> = {}) {
  return (argv: string[]) => {
    const command = argv[1]
    if (command === 'exec' || command === 'send' || command === 'review') return admitted()
    if (command === 'wait') return finished(over)
    if (command === 'status') return finished(over)
    if (command === 'result') return ok(output)
    throw new Error('unexpected agentcli call: ' + argv.join(' '))
  }
}

function subagent(w: any, id: string, type: string, rows: { role: 'user' | 'assistant'; text: string }[]) {
  w.agents.push({ id, type })
  w.messages[id] = rows
}

test('the mod registers one background agent type per scenario, only for when the user asks', async ($, on) => {
  const w = world(on)
  await $.session.start(START)

  expect(w.agentTypes.map((t) => t.name).sort()).toEqual(TYPES)
  for (const spec of w.agentTypes) {
    expect(spec.background).toBe(true)
    expect(spec.description).toContain('Use only when the user asks')
    // If the hook ever fails to answer, the stand-in model is the cheapest one
    // with a read-only tool list, and its prompt makes it say so.
    expect(spec.model).toBe('haiku')
    expect(spec.tools).toEqual(['Read'])
    expect(spec.prompt).toContain(HANDOFF_FAILED)
  }
})

test('the agent types are hidden from the model until the dispatch skill loads in the session', async ($, on) => {
  const w = world(on)
  await $.session.start(START)
  const offer = (agent: string) => $.agent.offer({ agent, description: '', source: 'plugin', provider: { plugin: 'agent-cli', tier: 'user' } } as any)

  expect(await offer('agent-cli:delegation')).toEqual({ isOffered: false })
  expect(await offer('Explore')).toEqual({ isOffered: true })

  await $.skill.prompt({ skill: 'agent-cli:other', text: '' })
  expect(await offer('agent-cli:delegation')).toEqual({ isOffered: false })

  await $.skill.prompt({ skill: 'agent-cli:dispatch', text: 'Dispatch' })
  expect(await offer('agent-cli:delegation')).toEqual({ isOffered: true })
  expect(w.runs).toEqual([])
})

test('a subagent of an agent type is answered by an agentcli job, never by the model', async ($, on) => {
  const w = world(on)
  w.respond = codex('Codex says the cache is safe.\n')
  subagent(w, 'ag-1', 'agent-cli:second-opinion', [{ role: 'user', text: 'Is a TTL cache safe here?' }])
  await $.session.start(START)

  const out = await step($, 'ag-1')

  expect(w.modelSteps).toEqual([])
  const admit = w.runs[0]!
  expect(isShim(admit.argv[0]!)).toBe(true)
  expect(admit.argv.slice(1)).toEqual(['exec', '--scenario', 'second-opinion', ...JOB_FLAGS, '-'])
  // The prompt travels on standard input, never inside an argument.
  expect(admit.stdin).toBe('Is a TTL cache safe here?')
  expect(w.runs.map((r) => r.argv[1])).toEqual(['exec', 'wait', 'status', 'result'])
  expect(out.text).toContain('Codex says the cache is safe.')
  expect(out.text).toContain('[agentcli run run-1 · conversation conv-1 · ok]')
  expect(out.result.answer).toBe(out.text)
  expect(out.result.stopReason).toBe('end_turn')
  expect(out.chunks.at(-1)).toMatchObject({ kind: 'stop', stopReason: 'end_turn' })
})

test('model requests of the main loop and of other agents reach the model untouched', async ($, on) => {
  const w = world(on)
  subagent(w, 'ag-x', 'Explore', [{ role: 'user', text: 'find it' }])
  await $.session.start(START)

  const main = await step($)
  const other = await step($, 'ag-x')

  expect(w.modelSteps).toEqual([undefined, 'ag-x'])
  expect(main.text).toBe('a Claude model answered')
  expect(other.text).toBe('a Claude model answered')
  expect(w.runs).toEqual([])
})

test('reminders the engine adds to the prompt are not sent to the agent', async ($, on) => {
  const w = world(on)
  w.respond = codex('done')
  subagent(w, 'ag-1', 'agent-cli:adhoc', [
    { role: 'user', text: '<system-reminder>\nsession context\n</system-reminder>\nSummarize README.md' },
  ])
  await $.session.start(START)

  await step($, 'ag-1')

  expect(w.runs[0]!.stdin).toBe('Summarize README.md')
})

test('a SendMessage to the subagent continues its agentcli conversation', async ($, on) => {
  const w = world(on)
  w.respond = codex('First answer.')
  subagent(w, 'ag-1', 'agent-cli:cross-check', [{ role: 'user', text: 'Check auth.go' }])
  await $.session.start(START)
  await step($, 'ag-1')

  w.messages['ag-1'] = [
    { role: 'user', text: 'Check auth.go' },
    { role: 'assistant', text: 'First answer.' },
    { role: 'user', text: 'Which finding is cheapest to fix?' },
  ]
  w.runs.length = 0
  const out = await step($, 'ag-1')

  expect(w.runs[0]!.argv.slice(1)).toEqual(['send', 'conv-1', ...JOB_FLAGS, '-'])
  expect(w.runs[0]!.stdin).toBe('Which finding is cheapest to fix?')
  expect(out.text).toContain('First answer.')
  expect(w.modelSteps).toEqual([])
})

test('a later turn whose conversation is not recorded is refused, not started afresh', async ($, on) => {
  const w = world(on)
  subagent(w, 'ag-1', 'agent-cli:adhoc', [
    { role: 'user', text: 'hi' },
    { role: 'assistant', text: 'hello' },
    { role: 'user', text: 'and now?' },
  ])
  await $.session.start(START)

  const out = await step($, 'ag-1')

  expect(out.text.startsWith(HANDOFF_FAILED)).toBe(true)
  expect(out.text).toContain('send tool')
  expect(w.runs).toEqual([])
  expect(w.modelSteps).toEqual([])
})

test('a code-review subagent reads its prompt as the review target', async ($, on) => {
  const w = world(on)
  w.respond = codex('[P2] auth.go:12 token not checked')
  subagent(w, 'ag-1', 'agent-cli:code-review', [{ role: 'user', text: 'base feature/login' }])
  subagent(w, 'ag-2', 'agent-cli:code-review', [{ role: 'user', text: ' uncommitted ' }])
  await $.session.start(START)

  await step($, 'ag-1')
  await step($, 'ag-2')

  const reviews = w.runs.filter((r) => r.argv[1] === 'review')
  expect(reviews.map((r) => r.argv.slice(1))).toEqual([
    ['review', '--scenario', 'code-review', '--base', 'feature/login', ...JOB_FLAGS],
    ['review', '--scenario', 'code-review', '--uncommitted', ...JOB_FLAGS],
  ])
  expect(reviews[0]!.stdin).toBeUndefined()
})

test('a code-review prompt that is not exactly one target is refused before agentcli runs', async ($, on) => {
  const w = world(on)
  for (const [id, text] of [['a', 'please review my changes'], ['b', 'base -rf'], ['c', 'commit']]) {
    subagent(w, id, 'agent-cli:code-review', [{ role: 'user', text }])
  }
  await $.session.start(START)

  for (const id of ['a', 'b', 'c']) {
    const out = await step($, id)
    expect(out.text.startsWith(HANDOFF_FAILED)).toBe(true)
    expect(out.text).toContain("'uncommitted', 'base <branch>' or 'commit <sha>'")
  }
  expect(w.runs).toEqual([])
  expect(w.modelSteps).toEqual([])
})

test('an agentcli failure becomes an answer that says the hand-off failed', async ($, on) => {
  const w = world(on)
  w.respond = () => ({ exitCode: 3, stdout: JSON.stringify({ sdk_status: 'conversation_busy', exit_code: 3, error: 'conversation c-1 has an active turn' }), stderr: '' })
  subagent(w, 'ag-1', 'agent-cli:delegation', [{ role: 'user', text: 'Rename foo to bar in util.go' }])
  subagent(w, 'ag-2', 'agent-cli:delegation', [{ role: 'user', text: 'Rename foo to bar in util.go' }])
  await $.session.start(START)

  const refused = await step($, 'ag-1')
  w.respond = () => {
    throw new Error('process timed out')
  }
  const thrown = await step($, 'ag-2')

  expect(refused.text.startsWith(HANDOFF_FAILED)).toBe(true)
  expect(refused.text).toContain('conversation c-1 has an active turn')
  // The harness reports a throwing process.run in its own words; what holds is
  // that the rejection still ends in a hand-off failure.
  expect(thrown.text.startsWith(HANDOFF_FAILED)).toBe(true)
  expect(w.modelSteps).toEqual([])
})

test('a long run is waited for in slices until it finishes', async ($, on) => {
  const w = world(on)
  const answers = codex('late answer')
  let waits = 0
  w.respond = (argv) => {
    if (argv[1] !== 'wait') return answers(argv)
    waits += 1
    return waits < 3 ? { exitCode: 5, stdout: JSON.stringify({ sdk_status: 'wait_timeout', exit_code: 5, error: 'run run-1 is still running after 9m0s' }), stderr: '' } : finished()
  }
  subagent(w, 'ag-1', 'agent-cli:adhoc', [{ role: 'user', text: 'slow one' }])
  await $.session.start(START)

  const out = await step($, 'ag-1')

  expect(waits).toBe(3)
  expect(w.runs.find((r) => r.argv[1] === 'wait')!.argv.slice(1)).toEqual(['wait', 'run-1', '--timeout', '540', '--json'])
  expect(out.text).toContain('late answer')
})

test('a run that did not deliver leads its answer with how it ended', async ($, on) => {
  const w = world(on)
  w.respond = codex('', { state: 'timeout', outcome: 'timeout', error_excerpt: 'stream disconnected' })
  subagent(w, 'ag-1', 'agent-cli:adhoc', [{ role: 'user', text: 'q' }])
  await $.session.start(START)

  const out = await step($, 'ag-1')

  expect(out.text.startsWith('agentcli run run-1 ended timeout: stream disconnected')).toBe(true)
  expect(out.text).toContain('· timeout]')
})

test('output over 64 KiB is cut and the answer names the output file', async ($, on) => {
  const w = world(on)
  w.respond = codex('x'.repeat(70000))
  subagent(w, 'ag-1', 'agent-cli:adhoc', [{ role: 'user', text: 'q' }])
  await $.session.start(START)

  const out = await step($, 'ag-1')

  expect(out.text).toContain('x'.repeat(65536) + '\n[output cut at 64 KiB; full output at /state/runs/run-1/output.md]')
  expect(out.text).not.toContain('x'.repeat(65537))
})

test('the conversations of two subagents are both recorded', async ($, on) => {
  const w = world(on)
  let n = 0
  w.respond = (argv) => {
    if (argv[1] === 'exec') {
      n += 1
      return admitted('run-' + n, 'conv-' + n)
    }
    return codex('ok')(argv)
  }
  subagent(w, 'ag-1', 'agent-cli:adhoc', [{ role: 'user', text: 'one' }])
  subagent(w, 'ag-2', 'agent-cli:adhoc', [{ role: 'user', text: 'two' }])
  await $.session.start(START)

  await Promise.all([step($, 'ag-1'), step($, 'ag-2')])

  expect(w.store.get('agent-conversations')).toEqual([
    { agent_id: 'ag-1', conversation_id: 'conv-1' },
    { agent_id: 'ag-2', conversation_id: 'conv-2' },
  ])
})

test('tool calls made inside an agent-cli subagent are refused', async ($, on) => {
  const w = world(on)
  subagent(w, 'ag-1', 'agent-cli:delegation', [{ role: 'user', text: 'q' }])
  subagent(w, 'ag-x', 'Explore', [{ role: 'user', text: 'q' }])
  await $.session.start(START)

  const inside: any = await $.tool.call({ tool: 'Bash', command: 'rm -rf build', agentId: 'ag-1' } as any)
  const other: any = await $.tool.call({ tool: 'Bash', command: 'ls', agentId: 'ag-x' } as any)
  const main: any = await $.tool.call({ tool: 'Bash', command: 'ls' } as any)

  expect(inside.deny).toContain('agent-cli agents run no tools')
  expect(other.result).toBe('unanswered')
  expect(main.result).toBe('unanswered')
})

test('a run that ended with exit code 5 is not waited for again', async ($, on) => {
  const w = world(on)
  const answers = codex('', { state: 'failed', outcome: 'error', error_excerpt: 'provider exited 5' })
  w.respond = (argv) => (argv[1] === 'wait' ? { ...finished({ state: 'failed', outcome: 'error' }), exitCode: 5 } : answers(argv))
  subagent(w, 'ag-1', 'agent-cli:adhoc', [{ role: 'user', text: 'q' }])
  await $.session.start(START)

  const out = await step($, 'ag-1')

  expect(w.runs.filter((r) => r.argv[1] === 'wait').length).toBe(1)
  expect(out.text.startsWith('agentcli run run-1 ended error: provider exited 5')).toBe(true)
})

test('an output that cannot be read is a hand-off failure, not an empty answer', async ($, on) => {
  const w = world(on)
  const answers = codex('')
  w.respond = (argv) => (argv[1] === 'result' ? { exitCode: 70, stdout: '', stderr: 'agentcli: reading output: permission denied\n' } : answers(argv))
  subagent(w, 'ag-1', 'agent-cli:adhoc', [{ role: 'user', text: 'q' }])
  await $.session.start(START)

  const out = await step($, 'ag-1')

  expect(out.text.startsWith(HANDOFF_FAILED)).toBe(true)
  expect(out.text).toContain('permission denied')
  expect(out.text).not.toContain('produced no output')
})

test('another session loading the dispatch skill does not close this session', async ($, on) => {
  const w = world(on)
  await $.session.start(START)
  const offer = () => $.agent.offer({ agent: 'agent-cli:adhoc', description: '', source: 'plugin', provider: { plugin: 'agent-cli', tier: 'user' } } as any)

  await $.skill.prompt({ skill: 'agent-cli:dispatch', text: '' })
  w.sessionId = 'sess-2'
  expect(await offer()).toEqual({ isOffered: false })
  await $.skill.prompt({ skill: 'agent-cli:dispatch', text: '' })
  w.sessionId = SESSION

  expect(await offer()).toEqual({ isOffered: true })
})
