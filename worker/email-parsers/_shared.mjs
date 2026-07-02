// Shared placeholder job-card extraction for all email-parsers/*.mjs modules.
// Prefixed with _ so it is never mistaken for a registered sender parser
// (mirrors providers/_http.mjs convention).
//
// PLACEHOLDER structure (T-240): real per-sender email templates differ and
// must be pinned against real inbox samples before this ships to production.
// Until then every sender's fixture uses the same
// `<a href="URL">TITLE</a> ... <span class="company">COMPANY</span>` shape.
//
// SECURITY (CRITICAL fix): email HTML is attacker-influenced content (any
// sender that gets past the from: allowlist can send arbitrary HTML). Two
// guards keep a crafted multi-MB message from stalling the worker's event
// loop for every job type, not just email ingest:
//   (a) MAX_HTML_LENGTH — reject oversized bodies outright (real job-alert
//       emails are small; 512KB is generous headroom).
//   (b) bounded quantifiers on the regex — the original `[\s\S]*?` gap
//       between </a> and <span class="company"> had no upper bound, so a
//       document with many anchors and no matching span forced a full
//       remaining-document rescan per anchor (measured: 1.65MB -> 6.6s,
//       O(n^2)). Capping every group's length makes each match attempt
//       O(bound) instead of O(remaining document), i.e. linear overall.
const MAX_HTML_LENGTH = 512 * 1024 // 512KB
const JOB_CARD_RE = /<a href="([^"]{1,2000})">([^<]{1,500})<\/a>[\s\S]{0,500}?<span class="company">([^<]{1,500})<\/span>/g

const HTML_ENTITIES = { '&amp;': '&', '&lt;': '<', '&gt;': '>', '&quot;': '"', '&#39;': "'" }

function decodeEntities(str) {
  return str.replace(/&amp;|&lt;|&gt;|&quot;|&#39;/g, (entity) => HTML_ENTITIES[entity])
}

export function parseJobCards(html) {
  if (!html) return []
  if (html.length > MAX_HTML_LENGTH) {
    // Thrown, not silently truncated: truncating mid-tag risks corrupt
    // matches, and the caller (ingest-email.mjs) already wraps parser.parse()
    // in a per-message try/catch that appends this to errors_json and moves
    // on to the next message (NFR-07) — never re-thrown further up.
    throw new Error(`email-parsers: html exceeds ${MAX_HTML_LENGTH}-byte cap (${html.length} bytes) — rejected`)
  }
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
