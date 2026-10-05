import { expect, test } from 'claude-code/testing'
import { listing, world } from './kit'

const START = { surface: 'terminal', isInteractive: true, cwd: '/work' } as const

test('the mod registers its three tools with schemas the model can use', async ($, on) => {
  const w = world(on)
  await $.session.start(START)

  expect(w.tools.map((t) => t.name).sort()).toEqual(['ask', 'jobs', 'send'])
  const spec = Object.fromEntries(w.tools.map((t) => [t.name, t]))
  for (const t of w.tools) {
    expect(t.description.length).toBeGreaterThan(20)
    expect(t.inputSchema.type).toBe('object')
  }
  expect(spec.ask.inputSchema.required).toEqual(['prompt'])
  expect(spec.send.inputSchema.required).toEqual(['conversation_id', 'prompt'])
  expect(spec.jobs.inputSchema.required).toEqual(['action'])
  expect(spec.jobs.inputSchema.properties.action.enum).toEqual(['list', 'status', 'result', 'cancel'])
  for (const name of ['ask', 'send']) {
    for (const key of ['prompt', 'provider', 'scenario', 'model', 'effort', 'sandbox', 'cwd']) {
      expect(spec[name].inputSchema.properties[key].type).toBe('string')
    }
  }
  expect(spec.send.inputSchema.properties.conversation_id.type).toBe('string')
  expect(spec.jobs.inputSchema.properties.run_id.type).toBe('string')
})

test('the mod registers exactly one command, /agent-cli-jobs, that runs mid-turn', async ($, on) => {
  const w = world(on)
  await $.session.start(START)

  expect(w.commands.length).toBe(1)
  expect(w.commands[0]).toMatchObject({ name: 'agent-cli-jobs', immediate: true })
})

test('starting the session twice registers the tools again without error', async ($, on) => {
  const w = world(on)
  await $.session.start(START)
  await $.session.start(START)

  expect(w.tools.length).toBe(6)
})

test('a command that is refused is logged and does not stop the session from starting', async ($, on) => {
  const w = world(on)
  w.refuseCommand = true
  w.respond = () => listing()
  await $.session.start(START)

  expect(w.tools.map((t) => t.name).sort()).toEqual(['ask', 'jobs', 'send'])
  expect(w.logs.length).toBe(1)
  expect(w.logs[0]).toContain('agent-cli-jobs')
  // The poll is running although the command was refused.
  await w.clock.advance(15000)
  expect(w.runs.length).toBe(1)
})
