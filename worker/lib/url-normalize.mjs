// Per-sender URL canonicalization so the same job reached via two
// tracking-wrapped links dedups against jobs' UNIQUE(user_id, url).
// Pure function — no network.

const TRACKING_PARAM_PREFIXES = ['utm_']
const TRACKING_PARAMS = new Set(['gclid', 'trk', 'trackingId', 'refId', 'from', 'vjs', 'Data'])

function stripTrackingParams(url) {
  for (const key of [...url.searchParams.keys()]) {
    if (TRACKING_PARAM_PREFIXES.some((p) => key.startsWith(p)) || TRACKING_PARAMS.has(key)) {
      url.searchParams.delete(key)
    }
  }
  return url
}

// Per-platform trusted hostname allowlist. A URL claiming to be platform X
// but hosted elsewhere (tracking/phishing redirect, spoofed sender) is
// rejected outright rather than rewritten/stripped — CRITICAL fix: without
// this, an attacker-controlled host with an id-shaped path (e.g.
// tracking.evil.com/jobs/view/999) got REWRITTEN into a fabricated
// https://www.linkedin.com/... canonical URL, and unrelated jobs collapsed
// onto the same canonical URL via ON CONFLICT DO NOTHING (silent data loss).
const HOST_RULES = {
  linkedin: /(^|\.)linkedin\.com$/i,
  indeed: /(^|\.)indeed\.com$/i, // covers country subdomains, e.g. pe.indeed.com
  computrabajo: /(^|\.)computrabajo\.com(\.\w+)?$/i, // covers ccSLDs, e.g. computrabajo.com.pe
  bumeran: /(^|\.)bumeran\.com(\.\w+)?$/i, // covers ccSLDs, e.g. bumeran.com.pe
}

const RULES = {
  linkedin(url) {
    const match = url.pathname.match(/\/jobs\/view\/(\d+)/)
    if (!match) return null
    return `https://www.linkedin.com/jobs/view/${match[1]}`
  },
  indeed(url) {
    const jk = url.searchParams.get('jk')
    if (!jk) return null
    return `https://www.indeed.com/viewjob?jk=${jk}`
  },
  computrabajo(url) {
    stripTrackingParams(url)
    return `${url.origin}${url.pathname}${url.search}`
  },
  bumeran(url) {
    stripTrackingParams(url)
    return `${url.origin}${url.pathname}${url.search}`
  },
}

/**
 * Normalize a raw job URL to a stable, dedup-safe canonical form.
 * @param {string} platform - parser id (e.g. 'linkedin', 'indeed', ...)
 * @param {string} rawUrl
 * @returns {string|null} canonical URL, or null if it cannot be parsed/extracted
 *   or trusted (caller must skip the job — never store an unvalidated URL).
 */
export function normalizeJobUrl(platform, rawUrl) {
  let url
  try {
    url = new URL(rawUrl)
  } catch {
    return null
  }

  // Trust boundary: only ever store http(s) links (reject javascript:, data:,
  // ftp:, etc. — email HTML is attacker-influenced content).
  if (url.protocol !== 'https:' && url.protocol !== 'http:') return null

  const hostAllow = HOST_RULES[platform]
  if (hostAllow) {
    // Known platform: the host MUST match its allowlist, or this is a
    // spoofed/tracking link claiming to be that platform — reject entirely.
    if (!hostAllow.test(url.hostname)) return null

    const canonical = RULES[platform](url)
    if (canonical) return canonical
    // Legit host, but the rule couldn't extract a canonical id (e.g. a
    // linkedin.com URL that isn't a /jobs/view/ link) — fall through to the
    // generic strip below; the host is already verified trustworthy.
  }

  if (!url.hostname) return null
  stripTrackingParams(url)
  return `${url.origin}${url.pathname}${url.search}`
}
