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
 */
export function normalizeJobUrl(platform, rawUrl) {
  let url
  try {
    url = new URL(rawUrl)
  } catch {
    return null
  }

  const rule = RULES[platform]
  if (rule) {
    const canonical = rule(url)
    if (canonical) return canonical
    // Rule matched a known platform but couldn't extract its id — fall through
    // to the generic fallback rather than dropping the job outright.
  }

  stripTrackingParams(url)
  return `${url.origin}${url.pathname}${url.search}`
}
