import { parseJobCards, makeSenderMatch } from './_shared.mjs'

// PLACEHOLDER sender address (T-240) — pin against a real inbox before production use.
const senders = ['no-reply@bumeran.com.pe']

export default {
  id: 'bumeran',
  senders,
  senderMatch: makeSenderMatch(senders),
  parse({ html }) {
    return parseJobCards(html)
  },
}
