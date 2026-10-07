import { expect, test } from 'claude-code/testing'
import { POLL_MS, SESSION, job, listing, ok, world } from './kit'

const START = { surface: 'terminal', isInteractive: true, cwd: '/work' } as const

test('the status line counts the running jobs of the session and clears at zero', async ($, on) => {
  const w = world(on)
  let runs: any[] = [job({ run_id: 'a', state: 'running' }), job({ run_id: 'b', state: 'queued' })]
  w.respond = () => listing(...runs)
  await $.session.start(START)

  await w.clock.advance(POLL_MS)
  expect(w.statuses.at(-1)).toContain('2')

  runs = [job({ run_id: 'a', state: 'running' }), job({ run_id: 'b', state: 'done', outcome: 'ok' })]
  w.respond = (argv) => (argv[1] === 'result' ? ok('') : listing(...runs))
  await w.clock.advance(POLL_MS)
  expect(w.statuses.at(-1)).toContain('1')

  runs = [job({ run_id: 'a', state: 'done', outcome: 'ok' }), job({ run_id: 'b', state: 'done', outcome: 'ok' })]
  await w.clock.advance(POLL_MS)
  expect(w.statuses.at(-1)).toBeUndefined()
})

test('foreground runs of the session are not counted as jobs', async ($, on) => {
  const w = world(on)
  w.respond = () => listing(job({ run_id: 'fg', state: 'running', background: false }))
  await $.session.start(START)

  await w.clock.advance(POLL_MS)

  expect(w.statuses.at(-1)).toBeUndefined()
})

test('the poll lists this session only, every fifteen seconds', async ($, on) => {
  const w = world(on)
  w.respond = () => listing()
  await $.session.start(START)

  await w.clock.advance(POLL_MS - 1)
  expect(w.runs.length).toBe(0)
  await w.clock.advance(1)
  expect(w.runs.length).toBe(1)
  expect(w.runs[0]!.argv.slice(1)).toEqual(['status', '--json', '--session-id', SESSION])
  await w.clock.advance(POLL_MS)
  expect(w.runs.length).toBe(2)
})

test('a tick is skipped while the previous one is still running', async ($, on) => {
  const w = world(on)
  w.respond = async () => {
    await w.clock.sleep(40000)
    return listing()
  }
  await $.session.start(START)

  await w.clock.advance(POLL_MS * 3)

  expect(w.runs.length).toBe(1)
})

test('a failing poll does not stop the next one', async ($, on) => {
  const w = world(on)
  let n = 0
  w.respond = (argv) => {
    if (argv[1] === 'result') return ok('done!')
    n += 1
    if (n === 1) throw new Error('process timed out')
    if (n === 2) return { exitCode: 70, stdout: '', stderr: 'boom' }
    return listing(job({ state: 'done', outcome: 'ok' }))
  }
  await $.session.start(START)

  await w.clock.advance(POLL_MS * 3)

  expect(w.submits.length).toBe(1)
})

test('/agentcli-jobs is registered to run mid-turn and prints the recent jobs at once', async ($, on) => {
  const w = world(on)
  w.respond = () =>
    listing(
      job({ run_id: 'run-2', state: 'running', scenario: 'code-review', conversation_id: 'conv-9' }),
      job({ run_id: 'run-1', state: 'done', outcome: 'ok' }),
      job({ run_id: 'fg', state: 'done', background: false }),
    )
  await $.session.start(START)

  const spec = w.commands.find((c) => c.name === 'agentcli-jobs')
  expect(spec).toBeDefined()
  expect(spec.immediate).toBe(true)
  expect(spec.description.length).toBeGreaterThan(0)

  const out: any = await $.command.run({ command: 'agentcli-jobs', args: '' })

  expect(w.runs[0]!.argv.slice(1)).toEqual(['status', '--json', '--session-id', SESSION])
  for (const part of ['run-2', 'running', 'code-review', 'conv-9', 'run-1', 'done']) expect(out.text).toContain(part)
  expect(out.text).not.toContain('fg')
})

test('/agentcli-jobs says so when the session has no jobs, and reports agentcli failures', async ($, on) => {
  const w = world(on)
  w.respond = () => listing()
  await $.session.start(START)

  const none: any = await $.command.run({ command: 'agentcli-jobs', args: '' })
  expect(none.text).toContain('No')

  w.respond = () => ({ exitCode: 70, stdout: '', stderr: 'agentcli: cannot resolve the home\n' })
  const failed: any = await $.command.run({ command: 'agentcli-jobs', args: '' })
  expect(failed.text).toContain('cannot resolve the home')
})

test('jobs started from the CLI are not counted in the status line', async ($, on) => {
  const w = world(on)
  w.respond = () =>
    listing(
      job({ run_id: 'mine', state: 'running' }),
      job({ run_id: 'theirs', state: 'running', source: 'cli' }),
      job({ run_id: 'hook', state: 'running', source: 'cr-parallel' }),
    )
  await $.session.start(START)

  await w.clock.advance(POLL_MS)

  expect(w.statuses.at(-1)).toContain('1')
  expect(w.statuses.at(-1)).not.toContain('3')
})
