// Shared stubs for the mod tests: a fake world beneath the mod where the
// agentcli binary, the store, the clock and the display calls are all in memory.
import { mock } from 'claude-code/testing'

export const SESSION = 'sess-1'
// The mod runs the plugin's own shim: an absolute path ending in bin/agentcli.
export const isShim = (p: string) => p.startsWith('/') && p.endsWith('/bin/agentcli')
export const TOOL = (name: string) => `mcp__agent-cli__${name}`

export type Run = { argv: string[]; stdin?: string; cwd?: string }
export type Reply = { exitCode: number; stdout: string; stderr: string }
export type Responder = (argv: string[], init: any) => Reply | Promise<Reply>

export function ok(stdout: string): Reply {
  return { exitCode: 0, stdout, stderr: '' }
}

export function job(over: Record<string, unknown> = {}) {
  return {
    run_id: 'run-1',
    conversation_id: 'conv-1',
    state: 'running',
    outcome: null,
    scenario: 'adhoc',
    background: true,
    source: 'mod',
    output_path: '/state/runs/run-1/output.md',
    admitted_at: '2026-01-01T00:00:00Z',
    ...over,
  }
}

export function listing(...runs: unknown[]) {
  return ok(JSON.stringify({ runs }))
}

// world registers every stub a test needs. Call it before the first call on $.
export function world(on: any, opts: { store?: Record<string, unknown> } = {}) {
  const w = {
    clock: mock.clock(on),
    runs: [] as Run[],
    toasts: [] as string[],
    logs: [] as string[],
    statuses: [] as (string | undefined)[],
    submits: [] as string[],
    tools: [] as any[],
    commands: [] as any[],
    store: new Map<string, unknown>(Object.entries(opts.store ?? {})),
    storeWrites: [] as { key: string; value: unknown }[],
    refuseCommand: false,
    respond: ((argv: string[]) => {
      throw new Error('unexpected agentcli call: ' + argv.join(' '))
    }) as Responder,
  }
  on('session.start', () => ({ cwd: '/work' }))
  on('session.id', () => ({ value: SESSION }))
  on('tool.register', (_$: any, e: any) => {
    w.tools.push(e)
    return { value: { tool: 'mcp__agent-cli__' + e.name } }
  })
  on('command.register', (_$: any, e: any) => {
    if (w.refuseCommand) return { deny: '"/' + e.name + '" refused: it is the built-in /' + e.name }
    w.commands.push(e)
    return { value: { command: e.name } }
  })
  on('ui.toast', (_$: any, e: any) => {
    w.toasts.push(e.text)
    return { value: undefined }
  })
  on('ui.log', (_$: any, e: any) => {
    w.logs.push(e.text)
    return { value: undefined }
  })
  on('ui.status', (_$: any, e: any) => {
    w.statuses.push(e.text)
    return { value: undefined }
  })
  on('prompt.submit', (_$: any, e: any) => {
    w.submits.push(e.text)
    return { text: e.text }
  })
  on('store.get', (_$: any, e: any) => ({ value: w.store.get(e.key) }))
  on('store.set', (_$: any, e: any) => {
    w.store.set(e.key, JSON.parse(JSON.stringify(e.value)))
    w.storeWrites.push({ key: e.key, value: e.value })
    return { value: undefined }
  })
  on('store.delete', (_$: any, e: any) => {
    w.store.delete(e.key)
    return { value: undefined }
  })
  on('store.keys', () => ({ value: [...w.store.keys()] }))
  on('process.run', async (_$: any, e: any) => {
    w.runs.push({ argv: [...e.argv], stdin: e.init?.stdin, cwd: e.init?.cwd })
    return { value: await w.respond([...e.argv], e.init ?? {}) }
  })
  // Anything the mod does not answer itself falls through to here.
  on('tool.call', () => ({ result: 'unanswered' }))
  return w
}

export const POLL_MS = 15000
