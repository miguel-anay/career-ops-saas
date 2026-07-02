import { parseJobCards, makeSenderMatch } from './_shared.mjs'

// PLACEHOLDER sender address (T-240) — pin against a real inbox before production use.
const senders = ['alert@indeed.com']

export default {
  id: 'indeed',
  senders,
  senderMatch: makeSenderMatch(senders),
  parse({ html }) {
    return parseJobCards(html)
  },
}
