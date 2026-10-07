import {
  POLL_MS,
  NOTIFIED_KEY,
  admittedJob,
  failureText,
  jobsCommand,
  jobsListing,
  noticeText,
  oldestFirst,
  readNotified,
  remember,
  runningCount,
  sessionJobs,
  shimPath,
  isTerminal,
  modJobs,
  RESULT_OUTPUT_BYTES,
  resultNote,
  cutBytes,
  statusText,
  toastText,
  turnCommand,
} from './lib.js'
import {
  ADMIT_TIMEOUT_MS,
  CONVERSATIONS_KEY,
  DISPATCH_SKILL,
  GATE_KEY,
  LOST_CONVERSATION,
  WAIT_STILL_RUNNING,
  WAIT_TIMEOUT_MS,
  agentSpecs,
  answerText,
  conversationOf,
  finishedRun,
  firstTurnCommand,
  followUpCommand,
  handoffFailure,
  hasAnswered,
  readConversations,
  rememberConversation,
  turnPrompt,
  typeOf,
  waitCommand,
} from './subagent.js'

const COMMAND = 'agent-cli-jobs'
const NO_TOOLS = 'agent-cli agents run no tools: an agentcli job answers for them.'

const TURN_PROPERTIES = {
  provider: { type: 'string', description: 'Provider to run (default: codex).' },
  scenario: { type: 'string', description: 'Label for the purpose of the run, such as code-review or delegation; it selects default model, effort and sandbox.' },
  model: { type: 'string', description: 'Model, overriding the scenario default.' },
  effort: { type: 'string', description: 'Reasoning effort, overriding the scenario default.' },
  sandbox: { type: 'string', description: 'Sandbox mode, such as read-only or workspace-write.' },
  cwd: { type: 'string', description: 'Working directory for the run (default: the session directory).' },
}

// The poll timer and its guard live for one load of the module; a reload
// evaluates this file again and starts from scratch.
let timer = null
let isPolling = false
let shownStatus = null
// The agent ids this load has resolved: the type name for one of this plugin's
// agent types, null for any other agent. A reload refills it from $.agent.list().
const knownTypes = new Map()
// Conversation records are read, changed and written back; one write at a time
// keeps two agents admitted together from dropping each other's record.
let conversationWrites = Promise.resolve()

export function register(on) {
  on('session.start', async ($, e, next) => {
    await registerTools($)
    await registerAgentTypes($)
    if (timer !== null) timer.cancel()
    timer = $.clock.every(POLL_MS, () => {
      void poll($)
    })
    // Last, because a taken command name makes the call throw.
    try {
      await $.command.register({
        name: COMMAND,
        description: 'List the agent-cli jobs of this session',
        immediate: true,
      })
    } catch (error) {
      // The tools and notices work without the command.
      $.ui.log('could not register /' + COMMAND + ': ' + String(error && error.message ? error.message : error), { to: 'debug' })
    }
    return next(e)
  })

  on('tool.call', { tool: 'mcp__agent-cli__ask' }, async ($, e) => startTurn($, e, 'ask'))
  on('tool.call', { tool: 'mcp__agent-cli__send' }, async ($, e) => startTurn($, e, 'send'))
  on('tool.call', { tool: 'mcp__agent-cli__jobs' }, async ($, e) => runJobsAction($, e))

  on('command.run', { command: 'agent-cli-jobs' }, async ($) => listJobs($))

  // The agent types. Every way these hooks can fail ends in an answer that
  // says the hand-off failed, never in a Claude model's answer.
  on('skill.prompt', async ($, e, next) => {
    if (e.skill === $.plugin.name + ':' + DISPATCH_SKILL) await $.store.set(GATE_KEY, await $.session.id())
    return next(e)
  })

  on('agent.offer', async ($, e, next) => {
    if (typeOf($.plugin.name, e.agent) === null) return next(e)
    return (await isGateOpen($)) ? next(e) : { isOffered: false }
  }).catch(($, e, next) => (next.called || typeOf($.plugin.name, e.agent) === null ? next(e) : { isOffered: false }))

  on('turn.step', async function* ($, e, next) {
    const type = await subagentTypeOf($, e.agentId)
    if (type === null) return yield* next(e)
    return yield* answer(e, await answerTurn($, e.agentId, type, next.signal))
  }).catch(async function* ($, e, next) {
    if (next.called || !isKnownSubagent(e.agentId)) return yield* next(e)
    return yield* answer(e, handoffFailure('the plugin hook failed: ' + failureOf(next.error)))
  })

  on('tool.call', async ($, e, next) => {
    if ((await subagentTypeOf($, e.agentId)) === null) return next(e)
    return { deny: NO_TOOLS }
  }).catch(($, e, next) => (next.called || !isKnownSubagent(e.agentId) ? next(e) : { deny: NO_TOOLS }))
}

async function registerTools($) {
  const tools = [
    {
      name: 'ask',
      description:
        'Start a new agent-cli conversation as a background job and return at once with {conversation_id, run_id}. ' +
        'A notice arrives in this session when the job finishes, so do not poll for it.',
      inputSchema: {
        type: 'object',
        properties: { prompt: { type: 'string', description: 'The prompt for the agent.' }, ...TURN_PROPERTIES },
        required: ['prompt'],
      },
    },
    {
      name: 'send',
      description:
        'Send the next turn of an existing agent-cli conversation as a background job and return at once with {conversation_id, run_id}. ' +
        'A notice arrives in this session when the job finishes.',
      inputSchema: {
        type: 'object',
        properties: {
          conversation_id: { type: 'string', description: 'The conversation to continue (a run id of that conversation also works).' },
          prompt: { type: 'string', description: 'The prompt for the next turn.' },
          ...TURN_PROPERTIES,
        },
        required: ['conversation_id', 'prompt'],
      },
    },
    {
      name: 'jobs',
      description:
        'Inspect agent-cli jobs: list this session\'s recent runs, read the state of one run, read the output of a finished run, or cancel a run.',
      inputSchema: {
        type: 'object',
        properties: {
          action: { type: 'string', enum: ['list', 'status', 'result', 'cancel'], description: 'What to do.' },
          run_id: { type: 'string', description: 'The run to act on; required for status, result and cancel.' },
        },
        required: ['action'],
      },
    },
  ]
  for (const tool of tools) {
    try {
      await $.tool.register(tool)
    } catch {
      // A tool that fails to register must not keep the others or the poll from starting.
    }
  }
}

// startTurn serves the ask and send tools.
async function startTurn($, e, kind) {
  const sessionId = await $.session.id()
  const built = turnCommand(kind, shimPath($.plugin.root), sessionId, e)
  if (built.error) return { deny: built.error }
  let reply
  try {
    reply = await $.process.run(built.argv, { stdin: built.stdin })
  } catch (error) {
    return { deny: 'agentcli could not run: ' + String(error && error.message ? error.message : error) }
  }
  if (reply.exitCode !== 0) return { deny: failureText(reply) }
  const ids = admittedJob(reply.stdout)
  if (ids === null) return { deny: 'agentcli printed an unexpected admission result: ' + reply.stdout.trim().slice(0, 500) }
  return { result: JSON.stringify(ids) }
}

// runJobsAction serves the jobs tool.
async function runJobsAction($, e) {
  const sessionId = await $.session.id()
  const built = jobsCommand(shimPath($.plugin.root), sessionId, e)
  if (built.error) return { deny: built.error }
  let reply
  try {
    reply = await $.process.run(built.argv)
  } catch (error) {
    return { deny: 'agentcli could not run: ' + String(error && error.message ? error.message : error) }
  }
  if (reply.exitCode !== 0) return { deny: failureText(reply) }
  if (e.action === 'cancel') return { result: reply.stdout.trim() || 'cancellation requested for ' + e.run_id }
  if (e.action === 'result') {
    const cut = cutBytes(reply.stdout, RESULT_OUTPUT_BYTES)
    if (!cut.isCut) return { result: reply.stdout || reply.stderr.trim() || 'The run produced no output.' }
    return { result: cut.text + '\n' + resultNote(await outputPathOf($, shimPath($.plugin.root), e.run_id)) }
  }
  return { result: reply.stdout }
}

// listJobs serves /agent-cli-jobs.
async function listJobs($) {
  try {
    const sessionId = await $.session.id()
    const reply = await $.process.run([shimPath($.plugin.root), 'status', '--json', '--session-id', sessionId])
    if (reply.exitCode !== 0) return { text: 'Could not list jobs. ' + failureText(reply) }
    return { text: jobsListing(sessionJobs(reply.stdout)) }
  } catch (error) {
    return { text: 'Could not list jobs. ' + String(error && error.message ? error.message : error) }
  }
}

// poll runs once per period and never overlaps itself: a period that arrives
// while the previous one is still working is skipped.
async function poll($) {
  if (isPolling) return
  isPolling = true
  try {
    await checkJobs($)
  } catch {
    // The next period tries again.
  } finally {
    isPolling = false
  }
}

async function checkJobs($) {
  const bin = shimPath($.plugin.root)
  const sessionId = await $.session.id()
  const reply = await $.process.run([bin, 'status', '--json', '--session-id', sessionId])
  if (reply.exitCode !== 0) return
  const jobs = modJobs(sessionJobs(reply.stdout))

  const text = statusText(runningCount(jobs))
  if (text !== shownStatus) {
    shownStatus = text
    $.ui.status(text)
  }

  let notified = readNotified(await $.store.get(NOTIFIED_KEY))
  for (const job of oldestFirst(jobs)) {
    if (!isTerminal(job.state) || notified.includes(job.run_id)) continue
    // Record the job before announcing it, so a failure while announcing can
    // never produce a second notice.
    notified = remember(notified, job.run_id)
    await $.store.set(NOTIFIED_KEY, notified)
    const output = await readOutput($, bin, job.run_id)
    $.ui.toast(toastText(job))
    $.prompt.submit({ text: noticeText(job, output) }).catch(() => {})
  }
}

async function readOutput($, bin, runId) {
  try {
    const reply = await $.process.run([bin, 'result', runId])
    return reply.exitCode === 0 ? reply.stdout : null
  } catch {
    return null
  }
}

// outputPathOf looks up where a run's output file is, or null when it cannot.
async function outputPathOf($, bin, runId) {
  try {
    const reply = await $.process.run([bin, 'status', runId, '--json'])
    if (reply.exitCode !== 0) return null
    const path = JSON.parse(reply.stdout).output_path
    return typeof path === 'string' && path !== '' ? path : null
  } catch {
    return null
  }
}

async function registerAgentTypes($) {
  for (const spec of agentSpecs()) {
    try {
      await $.agent.register(spec)
    } catch (error) {
      // One type that fails to register must not keep the others from it.
      $.ui.log('could not register agent type ' + spec.name + ': ' + messageOf(error), { to: 'debug' })
    }
  }
}

// The agent types are offered to the model only once the dispatch skill has
// loaded in this session.
async function isGateOpen($) {
  return (await $.store.get(GATE_KEY)) === (await $.session.id())
}

// subagentTypeOf returns this plugin's type name for the agent whose loop an
// event runs in, or null for the main loop and any other agent.
async function subagentTypeOf($, agentId) {
  if (agentId === undefined) return null
  if (knownTypes.has(agentId)) return knownTypes.get(agentId)

  const listed = (await $.agent.list()).find((agent) => agent.id === agentId)
  if (listed === undefined) return null

  const type = typeOf($.plugin.name, listed.type)
  knownTypes.set(agentId, type)
  return type
}

function isKnownSubagent(agentId) {
  return agentId !== undefined && typeof knownTypes.get(agentId) === 'string'
}

// answer streams one text block and ends the step, as a model's reply would.
async function* answer(e, text) {
  yield { kind: 'text', index: 0, text }
  yield { kind: 'stop', stopReason: 'end_turn', usage: null }
  return { turnId: e.turnId, index: e.index, answer: text, toolUses: [], stopReason: 'end_turn', usage: null }
}

async function answerTurn($, agentId, type, signal) {
  try {
    return await runSubagentTurn($, agentId, type, signal)
  } catch (error) {
    return handoffFailure(messageOf(error))
  }
}

async function runSubagentTurn($, agentId, type, signal) {
  const rows = await $.session.messages({ agentId })
  if (!Array.isArray(rows)) return handoffFailure('could not read the prompt: ' + rows.deny)

  const bin = shimPath($.plugin.root)
  const built = await subagentCommand($, bin, agentId, type, rows)
  if (built.error) return handoffFailure(built.error)

  const reply = await $.process.run(built.argv, { stdin: built.stdin, timeoutMs: ADMIT_TIMEOUT_MS })
  if (reply.exitCode !== 0) return handoffFailure(failureText(reply))
  const ids = admittedJob(reply.stdout)
  if (ids === null) return handoffFailure('agentcli printed an unexpected admission result: ' + reply.stdout.trim().slice(0, 500))

  await recordConversation($, agentId, ids.conversation_id)
  return collectRun($, bin, ids.run_id, signal)
}

// subagentCommand starts the conversation on the agent's first turn and
// continues it on every later one (a SendMessage to the agent).
async function subagentCommand($, bin, agentId, type, rows) {
  const sessionId = await $.session.id()
  const prompt = turnPrompt(rows)
  if (!hasAnswered(rows)) return firstTurnCommand(type, bin, sessionId, prompt)

  const conversationId = conversationOf(readConversations(await $.store.get(CONVERSATIONS_KEY)), agentId)
  if (conversationId === null) return { error: LOST_CONVERSATION }
  return followUpCommand(bin, sessionId, conversationId, prompt)
}

async function recordConversation($, agentId, conversationId) {
  const write = conversationWrites.then(async () => {
    const records = readConversations(await $.store.get(CONVERSATIONS_KEY))
    await $.store.set(CONVERSATIONS_KEY, rememberConversation(records, agentId, conversationId))
  })
  conversationWrites = write.catch(() => {})
  try {
    await write
  } catch (error) {
    // The run is already admitted: answer it, and only a follow-up loses out.
    $.ui.log('could not record the conversation of agent ' + agentId + ': ' + messageOf(error), { to: 'debug' })
  }
}

// collectRun waits for the run and answers with it. An interrupted turn (Esc,
// or TaskStop on the agent) cancels the run, including one interrupted before
// the wait began.
async function collectRun($, bin, runId, signal) {
  const cancel = () => {
    $.process.run([bin, 'cancel', runId]).catch(() => {})
  }
  if (signal.aborted) {
    cancel()
    return handoffFailure('the turn was interrupted, so run ' + runId + ' was cancelled.')
  }

  signal.addEventListener('abort', cancel, { once: true })
  try {
    await waitForRun($, bin, runId, signal)
  } finally {
    signal.removeEventListener('abort', cancel)
  }

  return finalAnswer($, bin, runId)
}

// waitForRun waits in slices, each under $.process.run's ten-minute bound,
// until the run is no longer running or the turn is interrupted.
async function waitForRun($, bin, runId, signal) {
  let reply
  do {
    reply = await $.process.run(waitCommand(bin, runId), { timeoutMs: WAIT_TIMEOUT_MS })
  } while (reply.exitCode === WAIT_STILL_RUNNING && !signal.aborted)
}

async function finalAnswer($, bin, runId) {
  const status = await $.process.run([bin, 'status', runId, '--json'])
  const run = status.exitCode === 0 ? finishedRun(status.stdout) : null
  if (run === null) return handoffFailure('could not read run ' + runId + '. ' + failureText(status))
  if (!isTerminal(run.state)) return handoffFailure('run ' + runId + ' is still ' + run.state + '; read it later with the agent-cli jobs tool.')

  const output = await $.process.run([bin, 'result', runId])
  return answerText(run, output.exitCode === 0 ? output.stdout : '')
}

// failureOf names why a hook failed, from the engine's HookFailure.
function failureOf(failure) {
  return failure?.message ?? failure?.kind ?? 'unknown'
}

function messageOf(error) {
  return String(error && error.message ? error.message : error)
}
