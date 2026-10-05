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

const COMMAND = 'agent-cli-jobs'

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

export function register(on) {
  on('session.start', async ($, e, next) => {
    await registerTools($)
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
