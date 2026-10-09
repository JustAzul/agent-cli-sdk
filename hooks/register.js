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
  AGENT_SOURCE,
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
  FULL_REFRESH,
  REFRESH_TIMEOUT_MS,
  SESSION_START_REFRESH,
  isRefreshedByCompaction,
  mergeRefreshRequests,
  refreshCommand,
  refreshOutcome,
} from './prices.js'
import { BAND_REFRESH_MS, bandLine, flaggedRuns, followedRuns, labelled, withProgress } from './band.js'
import { createSummarizer } from './summarizer.js'
import { usageAddCommand } from './summary.js'
import { ETA_TIMEOUT_MS, ETA_UNAVAILABLE, etaCommand, etaText, endedSince, finishedTail, isAnnounceable, listedRuns, spawnNotice } from './spawn.js'

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
// The runs already announced as spawned, newest last, kept across reloads.
const SPAWN_NOTIFIED = { plugin: 'agentcli', key: 'spawnNotified' }
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
// The summary calls recorded with `usage add`, and how many of them the session's
// cost had counted when it was last read. A failed recording is logged once until
// one succeeds.
let usageAdds = 0
let readUsageAdds = 0
let isRecordFailureLogged = false
// The recordings that failed, oldest first, each tried again on every poll. The
// list holds at most MAX_PENDING_RECORDINGS; a recording that finds it full pushes
// out the oldest, which is lost for good and shown in a toast once per load.
const MAX_PENDING_RECORDINGS = 100
const LOST_RECORDINGS_TOAST = 'some summary costs could not be recorded; the Claude cost is a lower bound'
const pendingRecordings = []
let isLossToastShown = false
const pricedModels = new Set()
// The spinners already logged, by request id.
const loggedSpinners = new Set()
// Runs whose progress could not be read, each logged once.
const loggedProgressFailures = new Set()
// The agent ids this load has resolved: the type name for one of this plugin's
// agent types, null for any other agent. A reload refills it from $.agent.list().
const knownTypes = new Map()
// The agents whose turn is running in this load, by agent id: the run each
// follows and its type name. The spinner of such an agent says what the run does.
const turnAgents = new Map()
// The directory the session runs in, for the ETA lookups.
let sessionCwd = ''
// When this load started: a finished run in its first listing is announced only
// if it ended since.
let loadedAt = 0
let spawnTimer = null
let isScanningSpawns = false
// The runs the scans of this load have seen, and whether a failed ETA lookup has
// been logged in this load.
let seenRuns = new Set()
let isEtaFailureLogged = false
// Whether this load has read its first listing, which announces only the runs
// still going or ended since the load.
let hasSpawnBaseline = false
let isScanFailureLogged = false
// Whether this load has logged a status read that failed at admission, a
// notice that could not be shown, and a listed run it had to skip.
let isTurnReadFailureLogged = false
let isNoticeFailureLogged = false
let isSkippedRunLogged = false
// Writes to the announced runs are read, changed and written back one at a time.
let spawnWrites = Promise.resolve()
// The progress summaries of this load, which the config of the load decides.
let summaries = createSummarizer({})
// Conversation records are read, changed and written back; one write at a time
// keeps two agents admitted together from dropping each other's record.
let conversationWrites = Promise.resolve()

export function register(on, options) {
  summaries = createSummarizer(options ?? {})
  pendingRecordings.length = 0
  isLossToastShown = false
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
    sessionCwd = e.cwd
    loadedAt = await $.clock.now()
    seenRuns = new Set()
    isEtaFailureLogged = false
    hasSpawnBaseline = false
    isScanFailureLogged = false
    isTurnReadFailureLogged = false
    isNoticeFailureLogged = false
    isSkippedRunLogged = false
    if (spawnTimer !== null) spawnTimer.cancel()
    spawnTimer = $.clock.every(BAND_REFRESH_MS, () => {
      void scanSpawns($)
    })
    requestRefresh($, SESSION_START_REFRESH)
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
    if (isRefreshedByCompaction(e)) requestRefresh($, FULL_REFRESH)
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

  // The spinner of an agentcli agent's turn says what its run is doing.
  on('ui.render', { component: 'Spinner' }, async ($, e, next) => {
    if (!loggedSpinners.has(e.requestId)) {
      loggedSpinners.add(e.requestId)
      $.ui.log('Spinner raised for ' + e.requestId + ' with the word ' + e.props.word, { to: 'debug' })
    }
    const agent = turnAgents.get(e.requestId)
    // A message the engine set is not the run's to replace.
    if (agent === undefined || e.props.message != null) return next(e)
    const message = summaries.shown(agent.runId) ?? 'agentcli · ' + agent.type
    return next({ ...e, props: { ...e.props, message } })
  }).catch(($, e, next) => next(e))

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
  const bin = shimPath($.plugin.root)
  const built = turnCommand(kind, bin, sessionId, e)
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
  startNotice($, () => notifyTurn($, bin, ids.run_id))
  return { result: JSON.stringify(ids) }
}

// startNotice runs the work of a notice from a timer, so it outlives the hook
// that admitted the run and nothing in the hook waits for it.
function startNotice($, work) {
  $.clock.after(0, () => {
    void work()
  })
}

// notifyTurn announces the run of an ask or send with the scenario and source
// the run itself reports. A run whose status cannot be read is left to the scan.
// It never throws.
async function notifyTurn($, bin, runId) {
  let failure
  try {
    const reply = await $.process.run([bin, 'status', runId, '--json'])
    if (reply.exitCode === 0) {
      const run = JSON.parse(reply.stdout)
      if (typeof run.scenario === 'string' && typeof run.source === 'string') return notifyAdmitted($, bin, runId, run.scenario, run.source)
      failure = 'the run reports no scenario or source'
    } else {
      failure = failureText(reply)
    }
  } catch (error) {
    failure = messageOf(error)
  }
  if (isTurnReadFailureLogged) return
  isTurnReadFailureLogged = true
  $.ui.log('could not read run ' + runId + ' to announce it: ' + failure + '; the next scan announces it', { to: 'debug' })
}

// scanSpawns reads the session's runs and announces the ones it has not seen.
// It never overlaps itself.
async function scanSpawns($) {
  if (isScanningSpawns) return
  isScanningSpawns = true
  try {
    const bin = shimPath($.plugin.root)
    const reply = await $.process.run([bin, 'status', '--json', '--session-id', await $.session.id()])
    if (reply.exitCode !== 0) return logScanFailure($, failureText(reply))
    const listed = listedRuns(reply.stdout)
    if (listed === null) return logScanFailure($, 'the listing could not be read')
    isScanFailureLogged = false
    const isBaseline = !hasSpawnBaseline
    const claimed = await claimListed($, listed)
    hasSpawnBaseline = true
    for (const run of claimed) {
      if (!isTerminal(run.state)) await showSpawn($, run, () => lookupEta($, bin, run.scenario))
      else if (!isBaseline || endedSince(run, loadedAt)) await showSpawn($, run, () => finishedTail(run))
    }
  } catch (error) {
    logScanFailure($, messageOf(error))
  } finally {
    isScanningSpawns = false
  }
}

// claimListed claims the listed runs a notice can be written for, oldest first,
// and returns the ones this call claimed. Every run is claimed before any is
// announced, so a notice that fails cannot leave a finished run of the baseline
// to be announced later.
async function claimListed($, listed) {
  const claimed = []
  for (const run of oldestFirst(listed)) {
    if (!isAnnounceable(run)) {
      logSkippedRun($, run.run_id)
      continue
    }
    if (await claimSpawn($, run.run_id)) claimed.push(run)
  }
  return claimed
}

// logSkippedRun logs the first listed run of the load that had to be skipped;
// a later listing that gives it what it lacked announces it.
function logSkippedRun($, runId) {
  if (isSkippedRunLogged) return
  isSkippedRunLogged = true
  $.ui.log('skipped run ' + runId + ' in the spawn scan: the listing gives it no scenario, source or state', { to: 'debug' })
}

// logScanFailure logs a failed scan once until a scan succeeds; the next period
// tries again either way.
function logScanFailure($, failure) {
  if (isScanFailureLogged) return
  isScanFailureLogged = true
  $.ui.log('could not list the runs of the session for spawn notices: ' + failure, { to: 'debug' })
}

// showSpawn shows the notice of a listed run. A notice that cannot be shown is
// logged and does not stop the scan.
async function showSpawn($, run, tail) {
  try {
    await announceSpawn($, run.scenario, run.source, await tail())
  } catch (error) {
    logNoticeFailure($, run.scenario, error)
  }
}

// logNoticeFailure logs the first notice of the load that could not be shown.
function logNoticeFailure($, scenario, error) {
  if (isNoticeFailureLogged) return
  isNoticeFailureLogged = true
  $.ui.log('could not show the spawn notice of ' + scenario + ': ' + messageOf(error), { to: 'debug' })
}

// claimSpawn records a run as announced and tells whether this call is the one
// that did: false when this load or an earlier one already has.
async function claimSpawn($, runId) {
  if (seenRuns.has(runId)) return false
  seenRuns.add(runId)
  let isNew = true
  const write = spawnWrites.then(async () => {
    const announced = readNotified((await $.state.get(SPAWN_NOTIFIED)).value)
    if (announced.includes(runId)) {
      isNew = false
      return
    }
    await $.state.set(SPAWN_NOTIFIED, remember(announced, runId))
  })
  spawnWrites = write.catch(() => {})
  try {
    await write
  } catch (error) {
    // Without the record the run is announced once for this load.
    $.ui.log('could not record the spawn notice of run ' + runId + ': ' + messageOf(error), { to: 'debug' })
  }
  return isNew
}

// notifyAdmitted announces a run this mod admitted itself, unless the scan has
// already. It never throws.
async function notifyAdmitted($, bin, runId, scenario, source) {
  try {
    if (!(await claimSpawn($, runId))) return
    await announceSpawn($, scenario, source, await lookupEta($, bin, scenario))
  } catch (error) {
    logNoticeFailure($, scenario, error)
  }
}

async function announceSpawn($, scenario, source, tail) {
  const text = spawnNotice(scenario, source, tail)
  await $.ui.toast(text)
  await $.ui.log(text)
}

// lookupEta is the ETA part of a notice, or the unavailable text when it could
// not be read (logged once per load).
async function lookupEta($, bin, scenario) {
  let failure
  try {
    const reply = await $.process.run(etaCommand(bin, scenario, sessionCwd), { timeoutMs: ETA_TIMEOUT_MS })
    const text = reply.exitCode === 0 ? etaText(reply.stdout) : null
    if (text !== null) return text
    failure = reply.exitCode === 0 ? 'unreadable output' : failureText(reply)
  } catch (error) {
    failure = messageOf(error)
  }
  if (!isEtaFailureLogged) {
    isEtaFailureLogged = true
    $.ui.log('could not read the ETA of ' + scenario + ': ' + failure, { to: 'debug' })
  }
  return ETA_UNAVAILABLE
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
    return { deny: 'agentcli could not run: ' + messageOf(error) }
  }
  if (reply.exitCode !== 0) return { deny: failureText(reply) }
  return jobsAnswer($, e, reply)
}

// jobsAnswer is the jobs tool's answer to an action whose command exited 0.
async function jobsAnswer($, e, reply) {
  if (e.action === 'cancel') return { result: reply.stdout.trim() || 'cancellation requested for ' + e.run_id }
  if (e.action !== 'result') return { result: reply.stdout }

  const cut = cutBytes(reply.stdout, RESULT_OUTPUT_BYTES)
  if (!cut.isCut) return { result: reply.stdout || reply.stderr.trim() || 'The run produced no output.' }
  return { result: cut.text + '\n' + resultNote(await outputPathOf($, shimPath($.plugin.root), e.run_id)) }
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

  setFollowed(followedRuns(followed, flaggedRuns(reply.stdout)))
  hasPolledRuns = true
  await refreshBand($)
  // Before the costs are read, so a recording that lands now is counted by them.
  await retryRecordings($)
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
  if (hasRunEnded || isCostRefreshDue || isPricesChanged || usageAdds !== readUsageAdds) {
    // Taken before the read: a refresh that changes the prices, or a call that is
    // recorded, during it is read by the next poll.
    const hadPricesChanged = isPricesChanged
    const addsBeforeRead = usageAdds
    isPricesChanged = false
    const cost = await sessionCost($, bin, sessionId)
    // A failed read leaves the key, so the next poll reads again.
    if (cost === null) {
      isPricesChanged = isPricesChanged || hadPricesChanged
    } else {
      isCostRefreshDue = hasRunEnded
      shownTerminal = terminal
      readUsageAdds = addsBeforeRead
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
// priced, or null when the cost could not be read (logged once), the run of
// agentcli itself failing included.
async function sessionCost($, bin, sessionId) {
  let failure
  try {
    const reply = await $.process.run(sessionStatsCommand(bin, sessionId))
    if (reply.exitCode === 0) {
      isCostFailureLogged = false
      askForPrices($, missingPrices(reply.stdout))
      return costText(reply.stdout)
    }
    failure = failureText(reply)
  } catch (error) {
    failure = messageOf(error)
  }
  if (!isCostFailureLogged) {
    isCostFailureLogged = true
    $.ui.log('could not read the session cost: ' + failure, { to: 'debug' })
  }
  return null
}

// refreshBand reads what each followed run did since the last read and shows
// the latest of it in the band, dropping a run once it ends. A failed read
// leaves the run as it was, and a failed refresh is logged once until one
// succeeds; the next refresh tries again either way.
async function refreshBand($) {
  if (!isBandDue()) return
  isRefreshingBand = true
  try {
    const read = await readFollowed($)
    // Applied to the list as it is now: a poll may have changed it meanwhile.
    setFollowed(withProgress(followed, read))
    const labels = await bandLabels($, read)
    const now = await $.clock.now()
    await showBand($, followed.map((run) => ({ run_id: run.run_id, text: bandLine(labelled(run, labels.get(run.run_id)), now) })))
    isBandFailureLogged = false
  } catch (error) {
    logBandFailure($, error)
  } finally {
    isRefreshingBand = false
  }
}

// isBandDue tells that a refresh has work: the first poll has read the session's
// runs, no refresh is running, and the band follows a run or still shows one.
function isBandDue() {
  return hasPolledRuns && !isRefreshingBand && (followed.length > 0 || shownBand !== '[]')
}

// readFollowed reads what each followed run did since its last read.
async function readFollowed($) {
  const bin = shimPath($.plugin.root)
  const read = new Map()
  for (const run of followed) read.set(run.run_id, await newProgress($, bin, run.run_id, run.from))
  return read
}

// bandLabels is what each followed run shows in place of its newest raw step
// while summaries are active: its label, or null before one. A run whose
// summaries are not active has no entry and keeps its raw step.
async function bandLabels($, read) {
  const labels = new Map()
  for (const run of followed) {
    if (!(await summaries.observe(summaryPorts($), run.run_id, read.get(run.run_id)?.text ?? []))) continue
    labels.set(run.run_id, summaries.shown(run.run_id))
  }
  return labels
}

function logBandFailure($, error) {
  if (isBandFailureLogged) return
  isBandFailureLogged = true
  $.ui.log('could not update the band above the prompt: ' + messageOf(error), { to: 'debug' })
}

// setFollowed replaces the runs the band follows; a run it lets go of keeps no
// summaries.
function setFollowed(next) {
  for (const run of followed) {
    if (!next.some((kept) => kept.run_id === run.run_id)) summaries.forget(run.run_id)
  }
  followed = next
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
  requestRefresh($, FULL_REFRESH)
}

// requestRefresh asks for a price refresh and returns at once: the refresh runs
// from a timer, never inside the hook or the poll that asked. Requests that
// reach it before it starts, or while one runs, come to a single queued one.
function requestRefresh($, request) {
  queuedRefresh = mergeRefreshRequests(queuedRefresh, request)
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
      await refreshPrices($, request)
    }
  } finally {
    isRefreshing = false
  }
}

// refreshPrices runs one refresh. A change to the prices makes the next poll
// read the session's cost again; a failure is logged once until one succeeds.
async function refreshPrices($, request) {
  let outcome
  try {
    const reply = await $.process.run(refreshCommand(shimPath($.plugin.root), request), { timeoutMs: REFRESH_TIMEOUT_MS })
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

// summaryPorts is what the summaries use of the engine.
function summaryPorts($) {
  return {
    surfaces: () => $.session.surfaces(),
    now: () => $.clock.now(),
    complete: (request) => $.model.complete(request),
    record: (usage, runId) => recordUsage($, usage, runId),
    log: (text) => $.ui.log(text, { to: 'debug' }),
    labelSettled: (runId) => {
      if ([...turnAgents.values()].some((agent) => agent.runId === runId)) $.ui.invalidate('ui.render')
    },
  }
}

// recordUsage hands a billed summary call to `usage add`. It never throws; a
// recording that fails is kept to be tried again, and its failure is logged once
// until one recording succeeds.
async function recordUsage($, usage, runId) {
  const failure = await addUsage($, usage, runId)
  if (failure === null) return
  keepRecording($, { usage, runId })
  logRecordFailure($, failure)
}

// addUsage runs `usage add` once and returns null when it recorded the call, else
// why it did not.
async function addUsage($, usage, runId) {
  try {
    const argv = usageAddCommand(shimPath($.plugin.root), await $.session.id(), runId)
    const reply = await $.process.run(argv, { stdin: JSON.stringify(usage) })
    if (reply.exitCode !== 0) return failureText(reply)
    isRecordFailureLogged = false
    usageAdds += 1
    return null
  } catch (error) {
    return messageOf(error)
  }
}

function logRecordFailure($, failure) {
  if (isRecordFailureLogged) return
  isRecordFailureLogged = true
  $.ui.log('could not record the usage of a summary call: ' + failure, { to: 'debug' })
}

// keepRecording adds a failed recording at the end of the list, pushing out the
// oldest when the list is full.
function keepRecording($, recording) {
  if (pendingRecordings.length >= MAX_PENDING_RECORDINGS) {
    pendingRecordings.shift()
    reportLostRecording($)
  }
  pendingRecordings.push(recording)
}

function reportLostRecording($) {
  $.ui.log('dropped the oldest pending summary recording: the list is full', { to: 'debug' })
  if (isLossToastShown) return
  isLossToastShown = true
  $.ui.toast(LOST_RECORDINGS_TOAST)
}

// retryRecordings tries the pending recordings again, oldest first, and stops at
// the first that fails again, which stays first.
async function retryRecordings($) {
  for (const recording of [...pendingRecordings]) {
    const failure = await addUsage($, recording.usage, recording.runId)
    if (failure !== null) return logRecordFailure($, failure)
    // A recording kept meanwhile may have pushed this one out of the list.
    const index = pendingRecordings.indexOf(recording)
    if (index !== -1) pendingRecordings.splice(index, 1)
  }
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
  } finally {
    turnAgents.delete(agentId)
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

  startNotice($, () => notifyAdmitted($, bin, ids.run_id, type, AGENT_SOURCE))
  turnAgents.set(agentId, { runId: ids.run_id, type })
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
    const progress = await newProgress($, bin, runId, from)
    from = progress.next
    // With summaries active the row shows labels, not the entries.
    const lines = (await summaries.observe(summaryPorts($), runId, progress.text)) ? summaries.take(runId) : progress.text
    for (const line of lines) yield thinking(line)

    const now = await $.clock.now()
    if (lines.length > 0) shownAt = now
    else if (now - shownAt >= HEARTBEAT_MS) {
      yield thinking(heartbeatLine(now - startedAt))
      shownAt = now
    }
  } while (isWaitTimeout(reply) && !signal.aborted)
  summaries.forget(runId)
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
