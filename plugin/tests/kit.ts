// Shared stubs for the mod tests: a fake world beneath the mod where the
// agentcli binary, the store, the clock and the display calls are all in memory.
import { mock } from 'claude-code/testing'

export const SESSION = 'sess-1'
// The mod runs the plugin's own shim: an absolute path ending in bin/agentcli.
export const isShim = (p: string) => p.startsWith('/') && p.endsWith('/bin/agentcli')
export const TOOL = (name: string) => `mcp__agentcli__${name}`

export type Run = { argv: string[]; stdin?: string; cwd?: string; timeoutMs?: number }
export type Reply = { exitCode: number; stdout: string; stderr: string }
export type Responder = (argv: string[], init: any) => Reply | Promise<Reply>

// The price refresh the mod runs on its own schedule: `<shim> prices refresh …`.
export const isPrices = (argv: readonly string[]) => argv[1] === 'prices'
export const pricesRuns = (w: { runs: Run[] }) => w.runs.filter((r) => isPrices(r.argv))
export const otherRuns = (w: { runs: Run[] }) => w.runs.filter((r) => !isPrices(r.argv))

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

export const NO_USAGE = { input_tokens: 0, output_tokens: 0, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 }

// The completions a model can answer with.
export function answered(text: string) {
  return { isAnswered: true, text, usage: { ...NO_USAGE, input_tokens: 458, output_tokens: 18 } }
}
export function apiError(status: number | null) {
  return { isAnswered: false, reason: 'api-error', status, error: status === 429 ? 'rate_limit' : 'unknown', usage: NO_USAGE }
}
export const emptyReply = () => ({ isAnswered: false, reason: 'empty-reply', usage: { ...NO_USAGE, input_tokens: 458, output_tokens: 2 } })

// world registers every stub a test needs. Call it before the first call on $.
export function world(on: any, opts: { store?: Record<string, unknown> } = {}) {
  const w = {
    clock: mock.clock(on),
    runs: [] as Run[],
    toasts: [] as string[],
    logs: [] as string[],
    // The texts sent to the debug log alone.
    debugLogs: [] as string[],
    // Every event name the plugin asked to draw again.
    invalidations: [] as string[],
    statuses: [] as (string | undefined)[],
    submits: [] as string[],
    tools: [] as any[],
    commands: [] as any[],
    store: new Map<string, unknown>(Object.entries(opts.store ?? {})),
    storeWrites: [] as { key: string; value: unknown }[],
    sessionId: SESSION,
    refuseCommand: false,
    respond: ((argv: string[]) => {
      throw new Error('unexpected agentcli call: ' + argv.join(' '))
    }) as Responder,
    // What `prices refresh` answers; by default a cache that is still fresh.
    respondPrices: (() => ok(JSON.stringify({ sdk_status: 'ok', exit_code: 0, ran: false, changed: false, reason: 'fresh' }))) as Responder,
    agentTypes: [] as any[],
    agents: [] as { id: string; type: string }[],
    messages: {} as Record<string, { role: 'user' | 'assistant'; text: string }[]>,
    // The agent id of every model request that reached the model beneath the
    // plugin; undefined for the main loop.
    modelSteps: [] as (string | undefined)[],
    spawns: [] as any[],
    // What a spawn answers; by default the agent starts.
    spawnAnswer: null as null | ((e: any) => any),
    // The surfaces the session draws on; none by default, so a test is headless
    // until it opts in with ['terminal'].
    surfaces: [] as string[],
    // Every completion that reached the model beneath the plugin, as the
    // request named it.
    modelRequests: [] as any[],
    // What a completion answers; by default a label.
    respondModel: ((): any => answered('Reading auth.go')) as (request: any) => any,
  }
  on('session.start', () => ({ cwd: '/work' }))
  on('session.id', () => ({ value: w.sessionId }))
  on('tool.register', (_$: any, e: any) => {
    w.tools.push(e)
    return { value: { tool: 'mcp__agentcli__' + e.name } }
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
    if (e.to === 'debug') w.debugLogs.push(e.text)
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
    w.runs.push({ argv: [...e.argv], stdin: e.init?.stdin, cwd: e.init?.cwd, timeoutMs: e.init?.timeoutMs })
    const answer = isPrices(e.argv) ? w.respondPrices : w.respond
    return { value: await answer([...e.argv], e.init ?? {}) }
  })
  // The compaction beneath the plugin: the engine's own, which keeps nothing.
  on('session.compact', () => ({ messages: [{ role: 'user', text: 'a summary', toolUses: [] }] }))
  on('agent.register', (_$: any, e: any) => {
    w.agentTypes.push(e)
    return { value: { agent: 'agentcli:' + e.name } }
  })
  on('agent.list', () => ({ value: w.agents.map((a) => ({ ...a, description: '', status: 'running' })) }))
  on('agent.spawn', (_$: any, e: any) => {
    w.spawns.push(e)
    if (w.spawnAnswer) return w.spawnAnswer(e)
    const agentId = 'ag-spawned-' + w.spawns.length
    w.agents.push({ id: agentId, type: e.subagentType ?? e.subagent_type })
    return { model: 'claude-haiku-4-5', agentId }
  })
  on('agent.offer', () => ({ isOffered: true }))
  on('skill.prompt', (_$: any, e: any) => ({ text: e.text }))
  on('session.messages', (_$: any, e: any) => {
    const rows = w.messages[e.agentId]
    return { value: rows ?? { deny: 'no agent ' + e.agentId } }
  })
  on('turn.step', async function* (_$: any, e: any) {
    w.modelSteps.push(e.agentId)
    yield { kind: 'text', index: 0, text: 'a Claude model answered' }
    yield { kind: 'stop', stopReason: 'end_turn', usage: null }
    return { turnId: e.turnId, index: e.index, answer: 'a Claude model answered', toolUses: [], stopReason: 'end_turn', usage: null }
  })
  on('session.surfaces', () => ({ value: w.surfaces }))
  on('model.complete', async (_$: any, e: any) => {
    w.modelRequests.push(e)
    return { value: await w.respondModel(e) }
  })
  // Anything the mod does not answer itself falls through to here.
  on('tool.call', () => ({ result: 'unanswered' }))
  on('ui.invalidate', (_$: any, e: any) => {
    w.invalidations.push(e.event)
    return { value: undefined }
  })
  // The engine's own spinner: the message while one overrides the word.
  on('ui.render', { component: 'Spinner' }, ($: any, e: any) => {
    const { Text } = $.ui.resolve(e)
    return h(Text, {}, e.props.message ?? e.props.word)
  })
  // The engine's own band above the prompt: a survey while one holds it,
  // otherwise empty.
  on('ui.render', { component: 'AbovePrompt' }, ($: any, e: any) => {
    const { Box, Text } = $.ui.resolve(e)
    return e.props.hasSurvey ? h(Box, {}, h(Text, {}, 'How is Claude doing this session?')) : h(Box, {})
  })
  return w
}

// step raises one model request in the loop of `agentId` (the main loop when
// undefined) and reads the whole response.
export async function step($: any, agentId?: string) {
  const stream = $.turn.step({ turnId: 'turn-1', index: 0, model: 'claude-haiku-4-5', messageCount: 1, agentId })
  const chunks: any[] = []
  // Read by hand: the step's result is the stream's return value, which a
  // for-await loop drops.
  let next = await stream.next()
  while (!next.done) {
    chunks.push(next.value)
    next = await stream.next()
  }
  const text = chunks.filter((c) => c.kind === 'text').map((c) => c.text).join('')
  return { chunks, result: next.value, text }
}

// The toasts that are not spawn notices.
export const withoutSpawnNotices = (toasts: string[]) => toasts.filter((text) => !text.startsWith('agentcli · spawned '))

export const POLL_MS = 15000
