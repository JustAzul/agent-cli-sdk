// Pure helpers for the session's cost: how a cost shows in the status line and
// the band, and how the price refresh is asked for and read. Nothing here
// touches the mods API, so these functions can be read and tested on their own.

import { failureText } from './lib.js'

// A refresh at session start skips a cache checked within this long; one asked
// for after a compaction, or for a model with no price, always checks.
export const SESSION_START_MAX_AGE = '24h'
// The price list is fetched within 20 seconds and the model catalog read within
// 10, so a refresh that outlasts this one is stuck.
export const REFRESH_TIMEOUT_MS = 60000

const DECIMAL = /^(\d+)(?:\.(\d*))?$/
const CENTS_PER_DOLLAR = 100n
// The digit of the remainder past the cents at which a cost rounds up: half a
// cent and over.
const ROUND_UP_FROM = 5

// formatCost shows a cost, a decimal string of US dollars, in dollars and cents
// without ever leaving integer arithmetic. A complete cost rounds half up, and
// shows `<$0.01` when it is above zero and below half a cent. An incomplete
// cost is only a lower bound, so it truncates to cents and is led by `≥`.
// Anything that is not a decimal string shows nothing (undefined).
export function formatCost(costUsd, isComplete) {
  const match = typeof costUsd === 'string' ? DECIMAL.exec(costUsd) : null
  if (match === null) return undefined

  const fraction = match[2] ?? ''
  const cents = BigInt(match[1]) * CENTS_PER_DOLLAR + BigInt(fraction.slice(0, 2).padEnd(2, '0'))
  if (!isComplete) return '≥' + dollars(cents)

  const rest = fraction.slice(2)
  const rounded = cents + (rest !== '' && Number(rest[0]) >= ROUND_UP_FROM ? 1n : 0n)
  const isAboveZero = /[1-9]/.test(match[1] + fraction)
  return rounded === 0n && isAboveZero ? '<$0.01' : dollars(rounded)
}

function dollars(cents) {
  return '$' + cents / CENTS_PER_DOLLAR + '.' + String(cents % CENTS_PER_DOLLAR).padStart(2, '0')
}

// refreshCommand is `prices refresh`; hasMaxAge lets a cache checked within the
// session-start window stand.
export function refreshCommand(bin, hasMaxAge) {
  const maxAge = hasMaxAge ? ['--max-age', SESSION_START_MAX_AGE] : []
  return [bin, 'prices', 'refresh', ...maxAge, '--json']
}

// mergeRefreshRequests is the one refresh a queued request and a new one come
// to: it may skip a fresh cache only if every request it stands for may.
export function mergeRefreshRequests(queued, request) {
  if (queued === null) return request
  return { hasMaxAge: queued.hasMaxAge && request.hasMaxAge }
}

// isRefreshedByCompaction tells a compaction of the main conversation, by the
// person or by the engine at its threshold, from one that is not: a plugin's,
// the ahead-of-time one, and a subagent's own.
export function isRefreshedByCompaction(e) {
  return (e.trigger === 'manual' || e.trigger === 'auto') && e.agentId === undefined
}

// refreshOutcome reads the reply of `prices refresh`: why it failed (null when
// it did not), and whether it changed the cache. A refresh that had nothing to
// do (fresh, busy, off) did not fail.
export function refreshOutcome(reply) {
  if (reply.exitCode !== 0) return { failure: failureText(reply), hasChanged: false }
  try {
    const report = JSON.parse(reply.stdout)
    if (report && typeof report === 'object') return { failure: null, hasChanged: report.ran === true && report.changed === true }
  } catch {
    // Reported below as unexpected output.
  }
  return { failure: 'agentcli printed an unexpected refresh report: ' + String(reply.stdout).trim().slice(0, 200), hasChanged: false }
}
