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
  LOST_CONVERSATION,
  HEARTBEAT_MS,
  SLICE_TIMEOUT_MS,
  agentSpecs,
  answerText,
  conversationOf,
  costText,
  finishedRun,
  firstTurnCommand,
  followUpCommand,
  handoffFailure,
  hasAnswered,
  heartbeatLine,
  isWaitTimeout,
  missingPrices,
  progressCommand,
  progressLine,
  readConversations,
  readProgress,
  rememberConversation,
  sessionStatsCommand,
  sliceWaitCommand,
  statusLine,
  terminalRunIds,
  turnPrompt,
  typeOf,
} from './subagent.js'
import {
  REFRESH_TIMEOUT_MS,
  isRefreshedByCompaction,
  mergeRefreshRequests,
  refreshCommand,
  refreshOutcome,
} from './prices.js'
import { BAND_REFRESH_MS, bandLine, flaggedRuns, followedRuns, withProgress } from './band.js'

const COMMAND = 'agentcli-jobs'
const NO_TOOLS = 'agentcli agents run no tools: an agentcli job answers for them.'
// Set once the dispatch skill has loaded in this session; the agent types are
// offered to the model only then. Session state is never shared with another
// session, so no session can close another's gate.
const DISPATCH_LOADED = { plugin: 'agentcli', key: 'dispatchLoaded' }
// The band's lines: one for each run another caller flagged with
// --agent-feedback that is still going. Session state, so the band redraws on
// each write and a reload keeps what it shows.
const HOOK_RUNS = { plugin: 'agentcli', key: 'hookRuns' }
// A step's response: what the run is doing as thinking, then the answer.
const THINKING_BLOCK = 0
const ANSWER_BLOCK = 1

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
// The flagged runs the band follows, what was read of each, and the lines it
// last wrote (null until this load writes). Until this load's first poll has
// read the session's runs, the band keeps what an earlier load wrote.
let bandTimer = null
let followed = []
let shownBand = null
let hasPolledRuns = false
let isRefreshingBand = false
let isBandFailureLogged = false
let shownStatus = null
// The terminal runs of the session when its cost was last read, and that cost.
// A run's telemetry record lands just after its terminal state, so the cost is
// read when another run ends and once more on the next poll.
let shownTerminal = ''
let shownCost = undefined
let isCostRefreshDue = false
let isCostFailureLogged = false
// The price refresh: at most one runs at a time, and a request that arrives
// meanwhile waits in a single queued one. The models this load has asked a
// refresh for are not asked for again, so a model no refresh can price does not
// keep the refresh going.
let isRefreshing = false
let queuedRefresh = null
let isRefreshFailureLogged = false
let isPricesChanged = false
const pricedModels = new Set()
// Runs whose progress could not be read, each logged once.
const loggedProgressFailures = new Set()
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
    if (bandTimer !== null) bandTimer.cancel()
    bandTimer = $.clock.every(BAND_REFRESH_MS, () => {
      void refreshBand($)
    })
    requestRefresh($, true)
    // Last, because a taken command name makes the call throw.
    try {
      await $.command.register({
        name: COMMAND,
        description: 'List the agentcli jobs of this session',
        immediate: true,
      })
    } catch (error) {
      // The tools and notices work without the command.
      $.ui.log('could not register /' + COMMAND + ': ' + String(error && error.message ? error.message : error), { to: 'debug' })
    }
    return next(e)
  })

  // A compaction can change what a session costs by way of a model it has not
  // used before, so the prices are looked at again. The refresh runs on a timer:
  // the compaction never waits for it.
  on('session.compact', async ($, e, next) => {
    if (isRefreshedByCompaction(e)) requestRefresh($, false)
    return next(e)
  }).catch(($, e, next) => next(e))

  on('tool.call', { tool: 'mcp__agentcli__ask' }, async ($, e) => startTurn($, e, 'ask'))
  on('tool.call', { tool: 'mcp__agentcli__send' }, async ($, e) => startTurn($, e, 'send'))
  on('tool.call', { tool: 'mcp__agentcli__jobs' }, async ($, e) => runJobsAction($, e))

  on('command.run', { command: 'agentcli-jobs' }, async ($) => listJobs($))

  // The agent types. Every way these hooks can fail ends in an answer that
  // says the hand-off failed, never in a Claude model's answer.
  on('skill.prompt', async ($, e, next) => {
    if (e.skill === $.plugin.name + ':' + DISPATCH_SKILL) await $.state.set(DISPATCH_LOADED, true)
    return next(e)
  })

  on('agent.offer', async ($, e, next) => {
    const type = typeOf($.plugin.name, e.agent)
    if (type === null) return next(e)
    return (await isOfferedHere($)) ? next(e) : { isOffered: false }
  }).catch(($, e, next) => (next.called || typeOf($.plugin.name, e.agent) === null ? next(e) : { isOffered: false }))

  on('turn.step', async function* ($, e, next) {
    const type = await subagentTypeOf($, e.agentId)
    if (type === null) return yield* next(e)
    yield thinking('agentcli · ' + type)
    return yield* answer(e, yield* followTurn($, e.agentId, type, next.signal))
  }).catch(async function* ($, e, next) {
    if (next.called || !isKnownSubagent(e.agentId)) return yield* next(e)
    return yield* answer(e, handoffFailure('the plugin hook failed: ' + failureOf(next.error)))
  })

  on('tool.call', async ($, e, next) => {
    if ((await subagentTypeOf($, e.agentId)) === null) return next(e)
    return { deny: NO_TOOLS }
  }).catch(($, e, next) => (next.called || !isKnownSubagent(e.agentId) ? next(e) : { deny: NO_TOOLS }))

  // The band above the prompt shows what the runs other callers flagged are
  // doing. Only the person sees it; their results stay with their callers.
  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const { value } = await $.state.get(HOOK_RUNS)
    const rows = Array.isArray(value) ? value : []
    if (rows.length === 0) return next(e)
    // Above whatever else the band holds (a survey, another plugin's line),
    // never in its place.
    const below = await next(e)
    const { Box, Text } = $.ui.resolve(e)
    const lines = rows.map((row) => h(Text, { dimColor: true, wrap: 'truncate-end' }, row.text))
    return h(Box, { flexDirection: 'column' }, ...lines, ...(below ? [below] : []))
  }).catch(($, e, next) => next(e))
}

async function registerTools($) {
  const tools = [
    {
      name: 'ask',
      description:
        'Start a new agentcli conversation as a background job and return at once with {conversation_id, run_id}. ' +
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
        'Send the next turn of an existing agentcli conversation as a background job and return at once with {conversation_id, run_id}. ' +
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
        'Inspect agentcli jobs: list this session\'s recent runs, read the state of one run, read the output of a finished run, or cancel a run.',
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

// listJobs serves /agentcli-jobs.
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

  followed = followedRuns(followed, flaggedRuns(reply.stdout))
  hasPolledRuns = true
  await refreshBand($)
  await showStatus($, bin, sessionId, runningCount(jobs), terminalRunIds(reply.stdout))

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

async function showStatus($, bin, sessionId, running, terminal) {
  const hasRunEnded = terminal !== shownTerminal
  if (hasRunEnded || isCostRefreshDue || isPricesChanged) {
    // Taken before the read: a refresh that changes the prices during it is read
    // by the next poll.
    const hadPricesChanged = isPricesChanged
    isPricesChanged = false
    const cost = await sessionCost($, bin, sessionId)
    // A failed read leaves the key, so the next poll reads again.
    if (cost === null) {
      isPricesChanged = isPricesChanged || hadPricesChanged
    } else {
      isCostRefreshDue = hasRunEnded
      shownTerminal = terminal
      shownCost = cost
    }
  }
  const text = statusLine(statusText(running), shownCost)
  if (text !== shownStatus) {
    shownStatus = text
    $.ui.status(text)
  }
}

// sessionCost is the status line's cost text, undefined when no run could be
// priced, or null when the cost could not be read (logged once).
async function sessionCost($, bin, sessionId) {
  const reply = await $.process.run(sessionStatsCommand(bin, sessionId))
  if (reply.exitCode === 0) {
    isCostFailureLogged = false
    askForPrices($, missingPrices(reply.stdout))
    return costText(reply.stdout)
  }
  if (!isCostFailureLogged) {
    isCostFailureLogged = true
    $.ui.log('could not read the session cost: ' + failureText(reply), { to: 'debug' })
  }
  return null
}

// refreshBand reads what each followed run did since the last read and shows
// the latest of it in the band, dropping a run once it ends. A failed read
// leaves the run as it was, and a failed refresh is logged once until one
// succeeds; the next refresh tries again either way.
async function refreshBand($) {
  if (!hasPolledRuns || isRefreshingBand || (followed.length === 0 && shownBand === '[]')) return
  isRefreshingBand = true
  try {
    const bin = shimPath($.plugin.root)
    const read = new Map()
    for (const run of followed) read.set(run.run_id, await newProgress($, bin, run.run_id, run.from))
    // Applied to the list as it is now: a poll may have changed it meanwhile.
    followed = withProgress(followed, read)
    const now = await $.clock.now()
    await showBand($, followed.map((run) => ({ run_id: run.run_id, text: bandLine(run, now) })))
    isBandFailureLogged = false
  } catch (error) {
    if (!isBandFailureLogged) {
      isBandFailureLogged = true
      $.ui.log('could not update the band above the prompt: ' + messageOf(error), { to: 'debug' })
    }
  } finally {
    isRefreshingBand = false
  }
}

async function showBand($, rows) {
  const text = JSON.stringify(rows)
  if (text === shownBand) return
  await $.state.set(HOOK_RUNS, rows)
  shownBand = text
}

// askForPrices asks for one refresh for the models that have no price in the
// cache yet, each model once per load.
function askForPrices($, models) {
  const unasked = models.filter((model) => !pricedModels.has(model))
  if (unasked.length === 0) return
  for (const model of unasked) pricedModels.add(model)
  requestRefresh($, false)
}

// requestRefresh asks for a price refresh and returns at once: the refresh runs
// from a timer, never inside the hook or the poll that asked. Requests that
// reach it before it starts, or while one runs, come to a single queued one.
function requestRefresh($, hasMaxAge) {
  queuedRefresh = mergeRefreshRequests(queuedRefresh, { hasMaxAge })
  if (isRefreshing) return
  isRefreshing = true
  $.clock.after(0, () => {
    void runRefreshes($)
  })
}

async function runRefreshes($) {
  try {
    while (queuedRefresh !== null) {
      const request = queuedRefresh
      queuedRefresh = null
      await refreshPrices($, request.hasMaxAge)
    }
  } finally {
    isRefreshing = false
  }
}

// refreshPrices runs one refresh. A change to the prices makes the next poll
// read the session's cost again; a failure is logged once until one succeeds.
async function refreshPrices($, hasMaxAge) {
  let outcome
  try {
    const reply = await $.process.run(refreshCommand(shimPath($.plugin.root), hasMaxAge), { timeoutMs: REFRESH_TIMEOUT_MS })
    outcome = refreshOutcome(reply)
  } catch (error) {
    outcome = { failure: messageOf(error), hasChanged: false }
  }

  if (outcome.failure === null) {
    isRefreshFailureLogged = false
    if (outcome.hasChanged) isPricesChanged = true
    return
  }
  if (!isRefreshFailureLogged) {
    isRefreshFailureLogged = true
    $.ui.log('could not refresh the price list: ' + outcome.failure, { to: 'debug' })
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
async function isOfferedHere($) {
  return (await $.state.get(DISPATCH_LOADED)).value === true
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

function thinking(line) {
  return { kind: 'thinking', index: THINKING_BLOCK, text: line + '\n' }
}

// answer streams the answer block and ends the step, as a model's reply would.
async function* answer(e, text) {
  yield { kind: 'text', index: ANSWER_BLOCK, text }
  yield { kind: 'stop', stopReason: 'end_turn', usage: null }
  return { turnId: e.turnId, index: e.index, answer: text, toolUses: [], stopReason: 'end_turn', usage: null }
}

// followTurn shows the turn's run as it works and returns the answer.
async function* followTurn($, agentId, type, signal) {
  try {
    return yield* runSubagentTurn($, agentId, type, signal)
  } catch (error) {
    return handoffFailure(messageOf(error))
  }
}

async function* runSubagentTurn($, agentId, type, signal) {
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
  yield* followOwnRun($, bin, ids.run_id, signal)
  return finalAnswer($, bin, ids.run_id)
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

// followOwnRun watches a run the agent itself started. Interrupting the turn
// (Esc, or TaskStop on the agent) cancels the run, including one interrupted
// before the wait began.
async function* followOwnRun($, bin, runId, signal) {
  const cancel = () => {
    $.process.run([bin, 'cancel', runId]).catch(() => {})
  }
  if (signal.aborted) {
    cancel()
    return
  }

  signal.addEventListener('abort', cancel, { once: true })
  try {
    yield* watchRun($, bin, runId, signal)
  } finally {
    signal.removeEventListener('abort', cancel)
  }
}

// watchRun waits for the run in short slices and, after each, streams what the
// provider did since the last as thinking, or a heartbeat when it has been
// quiet, until the run ends or the turn is interrupted. It never stops the run.
async function* watchRun($, bin, runId, signal) {
  const startedAt = await $.clock.now()
  let shownAt = startedAt
  let from = 0
  let reply
  do {
    reply = await $.process.run(sliceWaitCommand(bin, runId), { timeoutMs: SLICE_TIMEOUT_MS })
    const lines = await newProgress($, bin, runId, from)
    from = lines.next
    for (const line of lines.text) yield thinking(line)

    const now = await $.clock.now()
    if (lines.text.length > 0) shownAt = now
    else if (now - shownAt >= HEARTBEAT_MS) {
      yield thinking(heartbeatLine(now - startedAt))
      shownAt = now
    }
  } while (isWaitTimeout(reply) && !signal.aborted)
}

// newProgress reads the run's progress entries from `from` on, and the run's
// state and cost so far (no cost key when the read failed). A read that fails
// shows nothing new, leaves `from` where it was and is logged once per run: the
// wait goes on, and the answer does not depend on progress.
async function newProgress($, bin, runId, from) {
  let failure
  try {
    const reply = await $.process.run(progressCommand(bin, runId, from))
    const progress = reply.exitCode === 0 ? readProgress(reply.stdout) : null
    if (progress !== null) {
      return { next: progress.next, text: progress.entries.map(progressLine), state: progress.state, cost: progress.cost }
    }
    failure = failureText(reply)
  } catch (error) {
    failure = messageOf(error)
  }

  if (!loggedProgressFailures.has(runId)) {
    loggedProgressFailures.add(runId)
    $.ui.log('could not read the progress of run ' + runId + ': ' + failure, { to: 'debug' })
  }
  return { next: from, text: [], state: null }
}

async function finalAnswer($, bin, runId) {
  const status = await $.process.run([bin, 'status', runId, '--json'])
  const run = status.exitCode === 0 ? finishedRun(status.stdout) : null
  if (run === null) return handoffFailure('could not read run ' + runId + '. ' + failureText(status))
  if (!isTerminal(run.state)) return handoffFailure('run ' + runId + ' is still ' + run.state + '; read it later with the agentcli jobs tool.')

  const output = await $.process.run([bin, 'result', runId])
  if (output.exitCode !== 0) return handoffFailure('could not read the output of run ' + runId + '. ' + failureText(output))
  return answerText(run, output.stdout)
}

// failureOf names why a hook failed, from the engine's HookFailure.
function failureOf(failure) {
  return failure?.message ?? failure?.kind ?? 'unknown'
}

function messageOf(error) {
  return String(error && error.message ? error.message : error)
}
