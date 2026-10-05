import { expect, test } from 'claude-code/testing'
import { SESSION, TOOL, isShim, ok, world } from './kit'

test('send runs the next turn of a conversation as a background job', async ($, on) => {
  const w = world(on)
  w.respond = () => ok(JSON.stringify({ conversation_id: 'conv-1', run_id: 'run-2', state: 'running', run_dir: '/s/runs/run-2', output_path: '/s/runs/run-2/output.md' }))
  await $.session.start({ surface: 'terminal', isInteractive: true, cwd: '/work' })

  const out: any = await $.tool.call({ tool: TOOL('send'), conversation_id: 'conv-1', prompt: 'and now the tests', effort: 'low', cwd: '/repo' })

  const run = w.runs[0]!
  expect(isShim(run.argv[0]!)).toBe(true)
  expect(run.argv.slice(1)).toEqual([
    'send', 'conv-1', '--background', '--json', '--source', 'mod', '--session-id', SESSION, '--effort', 'low', '--cwd', '/repo', '-',
  ])
  expect(run.stdin).toBe('and now the tests')
  expect(JSON.parse(out.result)).toEqual({ conversation_id: 'conv-1', run_id: 'run-2' })
})

test('send reports a busy conversation as an error', async ($, on) => {
  const w = world(on)
  w.respond = () => ({ exitCode: 3, stdout: JSON.stringify({ sdk_status: 'busy', exit_code: 3, error: 'conversation conv-1 is busy: run run-1 is still queued or running' }), stderr: '' })
  await $.session.start({ surface: 'terminal', isInteractive: true, cwd: '/work' })

  const out: any = await $.tool.call({ tool: TOOL('send'), conversation_id: 'conv-1', prompt: 'again' })

  expect(out.deny).toContain('is busy')
})

test('send refuses a conversation id that could read as a flag', async ($, on) => {
  const w = world(on)
  await $.session.start({ surface: 'terminal', isInteractive: true, cwd: '/work' })

  const out: any = await $.tool.call({ tool: TOOL('send'), conversation_id: '--json', prompt: 'x' })

  expect(w.runs.length).toBe(0)
})
