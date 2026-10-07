import { expect, test } from 'claude-code/testing'
import { POLL_MS, job, listing, ok, world } from './kit'

const START = { surface: 'terminal', isInteractive: true, cwd: '/work' } as const

test('a job already in the notified set raises no second notice', async ($, on) => {
  const w = world(on, { store: { notified: ['run-1'] } })
  w.respond = () => listing(job({ state: 'done', outcome: 'ok' }))
  await $.session.start(START)

  await w.clock.advance(POLL_MS * 2)

  expect(w.toasts).toEqual([])
  expect(w.submits).toEqual([])
  expect(w.runs.some((r) => r.argv[1] === 'result')).toBe(false)
})

test('starting the session again, as a reload does, does not repeat a notice or double the poll', async ($, on) => {
  const w = world(on)
  w.respond = (argv) => (argv[1] === 'result' ? ok('out') : listing(job({ state: 'done', outcome: 'ok' })))
  await $.session.start(START)
  await w.clock.advance(POLL_MS)
  expect(w.submits.length).toBe(1)

  await $.session.start(START)
  const polls = () => w.runs.filter((r) => r.argv[1] === 'status').length
  const before = polls()
  await w.clock.advance(POLL_MS)

  expect(w.submits.length).toBe(1)
  expect(polls() - before).toBe(1) // one poll, not two
})

test('the notified set keeps the 500 most recent ids', async ($, on) => {
  const w = world(on)
  // The listing is newest first, as agentcli prints it.
  const runs = Array.from({ length: 501 }, (_, i) => job({ run_id: `r-${500 - i}`, state: 'done', outcome: 'ok' }))
  w.respond = (argv) => (argv[1] === 'result' ? ok('') : listing(...runs))
  await $.session.start(START)

  await w.clock.advance(POLL_MS)

  const set = w.store.get('notified') as string[]
  expect(set.length).toBe(500)
  expect(set).toContain('r-500')
  expect(set).toContain('r-1')
  expect(set).not.toContain('r-0')
  expect(w.submits.length).toBe(501)
})

test('a new id pushes the oldest out of a full set', async ($, on) => {
  const seeded = Array.from({ length: 500 }, (_, i) => `old-${i}`)
  const w = world(on, { store: { notified: seeded } })
  w.respond = (argv) => (argv[1] === 'result' ? ok('') : listing(job({ run_id: 'fresh', state: 'done', outcome: 'ok' })))
  await $.session.start(START)

  await w.clock.advance(POLL_MS)

  const set = w.store.get('notified') as string[]
  expect(set.length).toBe(500)
  expect(set.at(-1)).toBe('fresh')
  expect(set).not.toContain('old-0')
  expect(set).toContain('old-1')
})

test('a damaged notified entry is treated as an empty set', async ($, on) => {
  const w = world(on, { store: { notified: 'not-a-list' } })
  w.respond = (argv) => (argv[1] === 'result' ? ok('') : listing(job({ state: 'done', outcome: 'ok' })))
  await $.session.start(START)

  await w.clock.advance(POLL_MS)

  expect(w.submits.length).toBe(1)
  expect(w.store.get('notified')).toEqual(['run-1'])
})
