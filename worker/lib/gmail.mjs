// Gmail read-only client: refresh->access token exchange + messages.list/get + MIME decode.
//
// SSRF guard: every URL this module calls is asserted against a fixed host
// allowlist before the request fires (same guard style as providers/greenhouse.mjs).
// Never log tokens or message bodies.
import { fetchJson } from '../providers/_http.mjs'

const ALLOWED_GMAIL_HOSTS = new Set(['oauth2.googleapis.com', 'gmail.googleapis.com'])

const TOKEN_URL = 'https://oauth2.googleapis.com/token'
const GMAIL_API_BASE = 'https://gmail.googleapis.com/gmail/v1/users/me'

export function assertGmailUrl(url) {
  let parsed
  try {
    parsed = new URL(url)
  } catch {
    throw new Error(`gmail: invalid URL: ${url}`)
  }
  if (parsed.protocol !== 'https:') throw new Error(`gmail: URL must use HTTPS: ${url}`)
  if (!ALLOWED_GMAIL_HOSTS.has(parsed.hostname))
    throw new Error(`gmail: untrusted hostname "${parsed.hostname}" — must be one of: ${[...ALLOWED_GMAIL_HOSTS].join(', ')}`)
  return url
}

/**
 * Exchange a stored Gmail refresh token for a short-lived access token.
 * Never logs the refresh token, the request body, or the response.
 * @param {string} refreshToken
 * @returns {Promise<string>} access token
 */
export async function getAccessToken(refreshToken) {
  assertGmailUrl(TOKEN_URL)
  const body = new URLSearchParams({
    grant_type: 'refresh_token',
    refresh_token: refreshToken,
    client_id: process.env.GOOGLE_CLIENT_ID,
    client_secret: process.env.GOOGLE_CLIENT_SECRET,
  })
  const json = await fetchJson(TOKEN_URL, {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: body.toString(),
    redirect: 'error',
  })
  if (!json?.access_token) throw new Error('gmail: token exchange returned no access_token (token may be revoked)')
  return json.access_token
}

/**
 * List message ids matching a Gmail search query. `q` MUST be built from the
 * parser registry's allowlisted senders (privacy — never reads the full inbox).
 * @param {string} accessToken
 * @param {string} q
 * @param {number} [max]
 * @returns {Promise<Array<{id: string}>>}
 */
export async function listMessages(accessToken, q, max = 50) {
  const url = `${GMAIL_API_BASE}/messages?${new URLSearchParams({ q, maxResults: String(max) })}`
  assertGmailUrl(url)
  const json = await fetchJson(url, {
    headers: { authorization: `Bearer ${accessToken}` },
    redirect: 'error',
  })
  return Array.isArray(json?.messages) ? json.messages : []
}

/**
 * Fetch a full message payload by id.
 * @param {string} accessToken
 * @param {string} id
 */
export async function getMessage(accessToken, id) {
  const url = `${GMAIL_API_BASE}/messages/${id}?format=full`
  assertGmailUrl(url)
  return fetchJson(url, {
    headers: { authorization: `Bearer ${accessToken}` },
    redirect: 'error',
  })
}

function headerValue(headers, name) {
  const h = (headers || []).find((entry) => entry.name?.toLowerCase() === name.toLowerCase())
  return h?.value || ''
}

function walkParts(part, acc) {
  if (!part) return
  const mimeType = part.mimeType || ''
  if (part.body?.data && mimeType.startsWith('text/')) {
    const decoded = Buffer.from(part.body.data, 'base64url').toString('utf8')
    if (mimeType === 'text/html') acc.html += decoded
    else if (mimeType === 'text/plain') acc.text += decoded
  }
  for (const sub of part.parts || []) walkParts(sub, acc)
}

/**
 * Pure MIME decode: walks payload.parts[] recursively (handles nested
 * multipart/alternative) and extracts { from, subject, html, text }.
 * @param {object} message - full Gmail message resource (from getMessage)
 */
export function decodeMessage(message) {
  const headers = message?.payload?.headers
  const acc = { html: '', text: '' }
  walkParts(message?.payload, acc)
  return {
    from: headerValue(headers, 'from'),
    subject: headerValue(headers, 'subject'),
    html: acc.html,
    text: acc.text,
  }
}
