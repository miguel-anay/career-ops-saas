import { vi, describe, it, expect, beforeEach } from 'vitest'

const mockFetchJson = vi.fn()

vi.mock('../../providers/_http.mjs', () => ({
  fetchJson: mockFetchJson,
}))

const gmail = await import('../../lib/gmail.mjs')
const { getAccessToken, listMessages, getMessage, decodeMessage, assertGmailUrl } = gmail

function b64url(str) {
  return Buffer.from(str, 'utf8').toString('base64url')
}

describe('gmail.mjs', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    process.env.GOOGLE_CLIENT_ID = 'test-client-id'
    process.env.GOOGLE_CLIENT_SECRET = 'test-client-secret'
  })

  describe('getAccessToken', () => {
    it('POSTs to oauth2.googleapis.com/token and returns access_token', async () => {
      mockFetchJson.mockResolvedValue({ access_token: 'ya29.fake-access-token' })

      const token = await getAccessToken('refresh-token-123')

      expect(token).toBe('ya29.fake-access-token')
      expect(mockFetchJson).toHaveBeenCalledTimes(1)
      const [url, opts] = mockFetchJson.mock.calls[0]
      expect(url).toBe('https://oauth2.googleapis.com/token')
      expect(opts.method).toBe('POST')
      expect(opts.body).toContain('refresh_token=refresh-token-123')
      expect(opts.body).toContain('client_id=test-client-id')
    })

    it('throws when the response has no access_token (revoked token)', async () => {
      mockFetchJson.mockResolvedValue({ error: 'invalid_grant' })

      await expect(getAccessToken('revoked-token')).rejects.toThrow()
    })
  })

  describe('listMessages', () => {
    it('GETs messages.list with q and maxResults', async () => {
      mockFetchJson.mockResolvedValue({ messages: [{ id: 'm1' }, { id: 'm2' }] })

      const result = await listMessages('access-token', 'from:linkedin.com', 25)

      expect(result).toEqual([{ id: 'm1' }, { id: 'm2' }])
      const [url, opts] = mockFetchJson.mock.calls[0]
      expect(url).toContain('gmail.googleapis.com/gmail/v1/users/me/messages')
      expect(url).toContain('maxResults=25')
      expect(opts.headers.authorization).toBe('Bearer access-token')
    })

    it('returns [] when messages is absent', async () => {
      mockFetchJson.mockResolvedValue({})
      const result = await listMessages('access-token', 'from:x', 10)
      expect(result).toEqual([])
    })
  })

  describe('getMessage', () => {
    it('GETs messages/{id}?format=full', async () => {
      mockFetchJson.mockResolvedValue({ id: 'm1', payload: {} })
      const result = await getMessage('access-token', 'm1')
      expect(result).toEqual({ id: 'm1', payload: {} })
      const [url] = mockFetchJson.mock.calls[0]
      expect(url).toContain('/messages/m1')
      expect(url).toContain('format=full')
    })
  })

  describe('decodeMessage', () => {
    it('decodes a single-part base64url message', () => {
      const payload = {
        payload: {
          mimeType: 'text/plain',
          headers: [
            { name: 'From', value: 'alert@indeed.com' },
            { name: 'Subject', value: 'New jobs for you' },
          ],
          body: { data: b64url('Plain text job alert body') },
        },
      }

      const result = decodeMessage(payload)

      expect(result.from).toBe('alert@indeed.com')
      expect(result.subject).toBe('New jobs for you')
      expect(result.text).toBe('Plain text job alert body')
      expect(result.html).toBe('')
    })

    it('decodes a nested multipart/alternative message (html + text)', () => {
      const payload = {
        payload: {
          mimeType: 'multipart/mixed',
          headers: [
            { name: 'From', value: 'jobalerts-noreply@linkedin.com' },
            { name: 'Subject', value: 'Jobs matching your profile' },
          ],
          parts: [
            {
              mimeType: 'multipart/alternative',
              parts: [
                { mimeType: 'text/plain', body: { data: b64url('plain body') } },
                { mimeType: 'text/html', body: { data: b64url('<p>html body</p>') } },
              ],
            },
          ],
        },
      }

      const result = decodeMessage(payload)

      expect(result.from).toBe('jobalerts-noreply@linkedin.com')
      expect(result.subject).toBe('Jobs matching your profile')
      expect(result.text).toBe('plain body')
      expect(result.html).toBe('<p>html body</p>')
    })
  })

  describe('SSRF allowlist', () => {
    it('accepts allowlisted hosts', () => {
      expect(() => assertGmailUrl('https://gmail.googleapis.com/gmail/v1/users/me/messages')).not.toThrow()
      expect(() => assertGmailUrl('https://oauth2.googleapis.com/token')).not.toThrow()
    })

    it('rejects an off-allowlist host', () => {
      expect(() => assertGmailUrl('https://evil.example.com/gmail/v1/users/me/messages')).toThrow(/untrusted hostname/)
    })

    it('rejects non-https protocol', () => {
      expect(() => assertGmailUrl('http://gmail.googleapis.com/gmail/v1/users/me/messages')).toThrow(/HTTPS/)
    })
  })
})
