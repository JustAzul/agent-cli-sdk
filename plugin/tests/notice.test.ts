import { expect, test } from 'claude-code/testing'
import { POLL_MS, job, listing, ok, withoutSpawnNotices, world } from './kit'

const START = { surface: 'terminal', isInteractive: true, cwd: '/work' } as const
const KIB8 = 8192

function finished(over: Record<string, unknown> = {}) {
  return job({ state: 'done', outcome: 'ok', scenario: 'code-review', ...over })
}

test('a finished job shows a toast and submits a notice with its output inline', async ($, on) => {
  const w = world(on)
  w.respond = (argv) => (argv[1] === 'result' ? ok('Two findings.\n') : listing(finished()))
  await $.session.start(START)

  await w.clock.advance(POLL_MS)

  expect(w.toasts.length).toBe(1)
  expect(w.toasts[0]).toContain('run-1')
  expect(w.submits.length).toBe(1)
  expect(w.submits[0]!.startsWith('agentcli job run-1 (code-review, conversation conv-1) finished: ok')).toBe(true)
  expect(w.submits[0]).toContain('Two findings.')
  const read = w.runs.find((r) => r.argv[1] === 'result')!
  expect(read.argv.slice(1)).toEqual(['result', 'run-1'])
})

test('the notice of a failed job names its state when there is no outcome', async ($, on) => {
  const w = world(on)
  w.respond = (argv) => (argv[1] === 'result' ? ok('') : listing(finished({ state: 'failed', outcome: null })))
  await $.session.start(START)

  await w.clock.advance(POLL_MS)

  expect(w.submits[0]!.startsWith('agentcli job run-1 (code-review, conversation conv-1) finished: failed')).toBe(true)
})

test('running jobs and foreground runs raise no notice', async ($, on) => {
  const w = world(on)
  w.respond = () => listing(job({ run_id: 'r', state: 'running' }), finished({ run_id: 'fg', background: false }))
  await $.session.start(START)

  await w.clock.advance(POLL_MS)

  expect(withoutSpawnNotices(w.toasts)).toEqual([])
  expect(w.submits).toEqual([])
})

test('a job is announced once however many polls see it finished', async ($, on) => {
  const w = world(on)
  w.respond = (argv) => (argv[1] === 'result' ? ok('x') : listing(finished()))
  await $.session.start(START)

  await w.clock.advance(POLL_MS * 4)

  expect(w.submits.length).toBe(1)
  expect(w.store.get('notified')).toEqual(['run-1'])
})

test('output over 8 KiB is cut to its first 8 KiB and the notice names the output path', async ($, on) => {
  const w = world(on)
  const output = 'a'.repeat(KIB8) + 'Z'.repeat(12 * 1024)
  w.respond = (argv) => (argv[1] === 'result' ? ok(output) : listing(finished({ output_path: '/state/runs/run-1/output.md' })))
  await $.session.start(START)

  await w.clock.advance(POLL_MS)

  const text = w.submits[0]!
  expect(text).toContain('a'.repeat(KIB8))
  expect(text).not.toContain('Z')
  expect(text).toContain('/state/runs/run-1/output.md')
})

test('output of exactly 8 KiB is delivered whole, without the path note', async ($, on) => {
  const w = world(on)
  const output = 'c'.repeat(KIB8)
  w.respond = (argv) => (argv[1] === 'result' ? ok(output) : listing(finished()))
  await $.session.start(START)

  await w.clock.advance(POLL_MS)

  expect(w.submits[0]).toContain(output)
  expect(w.submits[0]).not.toContain('/state/runs/run-1/output.md')
})

test('the cut counts bytes and never splits a character', async ($, on) => {
  const w = world(on)
  const output = '€'.repeat(4000) // 3 bytes each: 12000 bytes
  w.respond = (argv) => (argv[1] === 'result' ? ok(output) : listing(finished()))
  await $.session.start(START)

  await w.clock.advance(POLL_MS)

  const text = w.submits[0]!
  expect(text).toContain('€'.repeat(2730)) // 8190 bytes
  expect(text).not.toContain('€'.repeat(2731))
  expect(text).not.toContain('�')
})

test('a notice is still sent when the output cannot be read', async ($, on) => {
  const w = world(on)
  w.respond = (argv) => (argv[1] === 'result' ? { exitCode: 70, stdout: '', stderr: 'opening the output: denied' } : listing(finished()))
  await $.session.start(START)

  await w.clock.advance(POLL_MS)

  expect(w.submits.length).toBe(1)
  expect(w.submits[0]).toContain('finished: ok')
  expect(w.submits[0]).toContain('/state/runs/run-1/output.md')
})

test('a job started from the CLI raises no notice, whatever its source', async ($, on) => {
  const w = world(on)
  w.respond = () =>
    listing(finished({ run_id: 'cli-job', source: 'cli' }), finished({ run_id: 'cr-job', source: 'cr-parallel' }), finished({ run_id: 'no-source', source: undefined }))
  await $.session.start(START)

  await w.clock.advance(POLL_MS * 2)

  expect(w.toasts).toEqual([])
  expect(w.submits).toEqual([])
  expect(w.store.get('notified')).toBeUndefined()
})
