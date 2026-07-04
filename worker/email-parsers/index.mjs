import linkedin from './linkedin.mjs'
import computrabajo from './computrabajo.mjs'
import bumeran from './bumeran.mjs'
import indeed from './indeed.mjs'

// Registry — single source of truth for both the Gmail `q=` filter
// (allSenders) and per-message sender resolution (findParserForSender).
const REGISTRY = { linkedin, computrabajo, bumeran, indeed }

export function getParsers() {
  return Object.values(REGISTRY)
}

export function allSenders() {
  return getParsers().flatMap((p) => p.senders)
}

export function findParserForSender(from) {
  return getParsers().find((p) => p.senderMatch(from)) || null
}
