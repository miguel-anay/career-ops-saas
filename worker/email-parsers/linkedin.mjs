import { parseJobCards, makeSenderMatch } from './_shared.mjs'

// PLACEHOLDER sender address (T-240) — pin against a real inbox before production use.
const senders = ['jobalerts-noreply@linkedin.com']

export default {
  id: 'linkedin',
  senders,
  senderMatch: makeSenderMatch(senders),
  parse({ html }) {
    return parseJobCards(html)
  },
}
