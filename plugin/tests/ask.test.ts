import { expect, test } from 'claude-code/testing'
import { SESSION, TOOL, isShim, ok, world } from './kit'

const ADMITTED = { conversation_id: 'conv-1', run_id: 'run-1', state: 'running', run_dir: '/s/runs/run-1', output_path: '/s/runs/run-1/output.md' }

test('ask admits a background job and returns its ids', async ($, on) => {
  const w = world(on)
  w.respond = () => ok(JSON.stringify(ADMITTED))
  await $.session.start({ surface: 'terminal', isInteractive: true, cwd: '/work' })

  const out: any = await $.tool.call({ tool: TOOL('ask'), prompt: 'review this' })

  // The status read that follows the admission is the spawn notice's.
  expect(w.runs.filter((r) => r.argv[1] === 'exec').length).toBe(1)
  const run = w.runs[0]!
  expect(isShim(run.argv[0]!)).toBe(true)
  expect(run.argv.slice(1)).toEqual(['exec', '--background', '--json', '--source', 'mod', '--session-id', SESSION, '-'])
  // The prompt travels on standard input, never inside an argument.
  expect(run.stdin).toBe('review this')
  expect(JSON.parse(out.result)).toEqual({ conversation_id: 'conv-1', run_id: 'run-1' })
})

test('ask passes the optional settings as flags', async ($, on) => {
  const w = world(on)
  w.respond = () => ok(JSON.stringify(ADMITTED))
  await $.session.start({ surface: 'terminal', isInteractive: true, cwd: '/work' })

  await $.tool.call({
    tool: TOOL('ask'), prompt: '--not-a-flag; $(rm -rf x)', provider: 'codex', scenario: 'delegation',
    model: 'm1', effort: 'high', sandbox: 'read-only', cwd: '/repo',
  })

  expect(w.runs[0]!.argv.slice(1)).toEqual([
    'exec', '--background', '--json', '--source', 'mod', '--session-id', SESSION,
    '--provider', 'codex', '--scenario', 'delegation', '--model', 'm1', '--effort', 'high',
    '--sandbox', 'read-only', '--cwd', '/repo', '-',
  ])
  expect(w.runs[0]!.stdin).toBe('--not-a-flag; $(rm -rf x)')
})

test('ask turns an agentcli failure into an error the model can read', async ($, on) => {
  const w = world(on)
  w.respond = () => ({ exitCode: 2, stdout: JSON.stringify({ sdk_status: 'usage_error', exit_code: 2, error: 'unknown provider "x"' }), stderr: 'agentcli: unknown provider "x"\n' })
  await $.session.start({ surface: 'terminal', isInteractive: true, cwd: '/work' })

  const out: any = await $.tool.call({ tool: TOOL('ask'), prompt: 'hi', provider: 'x' })

  expect(out.deny).toContain('unknown provider "x"')
  expect(out.deny).toContain('2')
})

test('ask rejects an empty prompt without running agentcli', async ($, on) => {
  const w = world(on)
  await $.session.start({ surface: 'terminal', isInteractive: true, cwd: '/work' })

  const out: any = await $.tool.call({ tool: TOOL('ask'), prompt: '   ' })

  expect(w.runs.length).toBe(0)
  expect(out.deny).toContain('prompt')
})
