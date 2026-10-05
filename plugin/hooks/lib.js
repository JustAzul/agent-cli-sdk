// Pure helpers for the agent-cli mod. Nothing here touches the mods API: every
// call that reaches Claude Code lives in register.js, so these functions can be
// read and tested on their own.

export const POLL_MS = 15000
export const NOTICE_OUTPUT_BYTES = 8192
export const RESULT_OUTPUT_BYTES = 65536
export const JOB_SOURCE = 'mod'
export const NOTIFIED_LIMIT = 500
export const NOTIFIED_KEY = 'notified'

const TERMINAL_STATES = ['done', 'failed', 'cancelled', 'timeout', 'lost']
const JOBS_ACTIONS = ['list', 'status', 'result', 'cancel']
// Ids start with a letter, digit or underscore so they can never read as a flag.
const ID_PATTERN = /^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$/
const OPTIONAL_FLAGS = ['provider', 'scenario', 'model', 'effort', 'sandbox', 'cwd']

export function isTerminal(state) {
  return TERMINAL_STATES.includes(state)
}

export function shimPath(pluginRoot) {
  return pluginRoot + '/bin/agentcli'
}

function isId(value) {
  return typeof value === 'string' && ID_PATTERN.test(value)
}

function fail(error) {
  return { error }
}

// turnCommand builds the argv and standard input of a command that starts a
// background turn: `ask` opens a conversation, `send` continues one. The prompt
// goes on standard input so no prompt text ever becomes part of an argument.
export function turnCommand(kind, bin, sessionId, input) {
  const args = input ?? {}
  if (typeof args.prompt !== 'string' || args.prompt.trim() === '') {
    return fail('prompt is required and must be a non-empty string')
  }
  const argv = [bin]
  if (kind === 'send') {
    if (!isId(args.conversation_id)) {
      return fail('conversation_id is required and must be an id: letters, digits, ".", "_" or "-", not starting with "-" or "."')
    }
    argv.push('send', args.conversation_id)
  } else {
    argv.push('exec')
  }
  argv.push('--background', '--json', '--source', 'mod', '--session-id', sessionId)
  for (const name of OPTIONAL_FLAGS) {
    const value = args[name]
    if (value === undefined || value === null || value === '') continue
    if (typeof value !== 'string') return fail(name + ' must be a string')
    argv.push('--' + name, value)
  }
  argv.push('-')
  return { argv, stdin: args.prompt }
}

// jobsCommand maps one `jobs` tool call to the agentcli command that answers it.
export function jobsCommand(bin, sessionId, input) {
  const args = input ?? {}
  if (!JOBS_ACTIONS.includes(args.action)) {
    return fail('action must be one of: ' + JOBS_ACTIONS.join(', '))
  }
  if (args.action === 'list') {
    return { argv: [bin, 'status', '--json', '--session-id', sessionId] }
  }
  if (!isId(args.run_id)) {
    return fail('run_id is required for ' + args.action + ' and must be a run id: letters, digits, ".", "_" or "-", not starting with "-" or "."')
  }
  if (args.action === 'status') return { argv: [bin, 'status', args.run_id, '--json'] }
  return { argv: [bin, args.action, args.run_id] }
}

// failureText turns a non-zero agentcli exit into one line the model can act
// on: the JSON error object when --json produced one, else what it wrote to
// standard error.
export function failureText(reply) {
  let detail = ''
  let status = ''
  try {
    const parsed = JSON.parse(reply.stdout)
    if (parsed && typeof parsed.error === 'string') detail = parsed.error
    if (parsed && typeof parsed.sdk_status === 'string') status = parsed.sdk_status
  } catch {
    // Not JSON: fall back to the plain text below.
  }
  if (detail === '') {
    detail = String(reply.stderr || reply.stdout || '').trim().replace(/^agentcli: /, '')
  }
  const head = 'agentcli exited ' + reply.exitCode + (status ? ' (' + status + ')' : '')
  return detail === '' ? head + ' with no message' : head + ': ' + detail
}

// admittedJob reads the ids out of a successful background admission.
export function admittedJob(stdout) {
  try {
    const parsed = JSON.parse(stdout)
    if (parsed && typeof parsed.conversation_id === 'string' && typeof parsed.run_id === 'string') {
      return { conversation_id: parsed.conversation_id, run_id: parsed.run_id }
    }
  } catch {
    // Reported below as unexpected output.
  }
  return null
}

// sessionJobs keeps the background jobs of a `status --json` listing. Runs the
// session made in the foreground share its session id but are not jobs.
export function sessionJobs(stdout) {
  try {
    const parsed = JSON.parse(stdout)
    const runs = parsed && Array.isArray(parsed.runs) ? parsed.runs : []
    return runs.filter((run) => run && run.background === true && typeof run.run_id === 'string')
  } catch {
    return []
  }
}

// modJobs keeps the jobs this mod started. A job started from the CLI, a hook
// or a skill belongs to whoever started it, who collects it with `wait`.
export function modJobs(jobs) {
  return jobs.filter((job) => job.source === JOB_SOURCE)
}

export function runningCount(jobs) {
  return jobs.filter((job) => !isTerminal(job.state)).length
}

export function statusText(count) {
  if (count <= 0) return undefined
  return count + (count === 1 ? ' job running' : ' jobs running')
}

// oldestFirst orders a listing, which agentcli prints newest first, so that
// notices and the notified set follow the order the jobs were admitted in.
export function oldestFirst(jobs) {
  return [...jobs].reverse().sort((a, b) => {
    const left = String(a.admitted_at ?? '')
    const right = String(b.admitted_at ?? '')
    return left < right ? -1 : left > right ? 1 : 0
  })
}

// readNotified returns the persisted set of announced run ids, or an empty set
// when the stored value is missing or damaged.
export function readNotified(stored) {
  return Array.isArray(stored) ? stored.filter((id) => typeof id === 'string') : []
}

// remember appends an id and keeps only the most recent NOTIFIED_LIMIT.
export function remember(notified, id) {
  return [...notified, id].slice(-NOTIFIED_LIMIT)
}

// cutBytes returns the first `limit` bytes of text, never ending inside a
// multi-byte character.
export function cutBytes(text, limit) {
  const bytes = new TextEncoder().encode(text)
  if (bytes.length <= limit) return { text, isCut: false }
  let end = limit
  while (end > 0 && (bytes[end] & 0xc0) === 0x80) end -= 1
  return { text: new TextDecoder().decode(bytes.subarray(0, end)), isCut: true }
}

export function noticeText(job, output) {
  const outcome = job.outcome ?? job.state
  const head = 'agent-cli job ' + job.run_id + ' (' + job.scenario + ', conversation ' + job.conversation_id + ') finished: ' + outcome
  if (output === null) {
    return head + '\n\nThe output could not be read; it is at ' + job.output_path
  }
  if (output.trim() === '') return head
  const cut = cutBytes(output, NOTICE_OUTPUT_BYTES)
  if (!cut.isCut) return head + '\n\n' + output
  return head + '\n\n' + cut.text + '\n\n[output cut at 8 KiB; full output at ' + job.output_path + ']'
}

export function toastText(job) {
  return 'job ' + job.run_id + ' finished: ' + (job.outcome ?? job.state)
}

// resultNote closes a `jobs result` answer that was cut at 64 KiB.
export function resultNote(outputPath) {
  return outputPath ? '[output cut at 64 KiB; full output at ' + outputPath + ']' : '[output cut at 64 KiB]'
}

// jobsListing is what /agent-cli-jobs prints.
export function jobsListing(jobs) {
  if (jobs.length === 0) return 'No jobs in this session.'
  const lines = jobs.map((job) => [job.run_id, job.state, job.scenario, 'conversation ' + job.conversation_id].join('  '))
  return 'Jobs in this session (' + jobs.length + '):\n' + lines.join('\n')
}
