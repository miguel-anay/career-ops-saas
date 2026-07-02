// Shared placeholder job-card extraction for all email-parsers/*.mjs modules.
// Prefixed with _ so it is never mistaken for a registered sender parser
// (mirrors providers/_http.mjs convention).
//
// PLACEHOLDER structure (T-240): real per-sender email templates differ and
// must be pinned against real inbox samples before this ships to production.
// Until then every sender's fixture uses the same
// `<a href="URL">TITLE</a> ... <span class="company">COMPANY</span>` shape.
const JOB_CARD_RE = /<a href="([^"]+)">([^<]+)<\/a>[\s\S]*?<span class="company">([^<]+)<\/span>/g

const HTML_ENTITIES = { '&amp;': '&', '&lt;': '<', '&gt;': '>', '&quot;': '"', '&#39;': "'" }

function decodeEntities(str) {
  return str.replace(/&amp;|&lt;|&gt;|&quot;|&#39;/g, (entity) => HTML_ENTITIES[entity])
}

export function parseJobCards(html) {
  if (!html) return []
  const jobs = []
  for (const match of html.matchAll(JOB_CARD_RE)) {
    const [, url, title, company] = match
    jobs.push({ title: decodeEntities(title.trim()), company: decodeEntities(company.trim()), url: decodeEntities(url) })
  }
  return jobs
}

export function makeSenderMatch(senders) {
  return function senderMatch(from) {
    const lower = (from || '').toLowerCase()
    return senders.some((s) => lower.includes(s))
  }
}
