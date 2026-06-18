import { vi, describe, it, expect, beforeEach } from 'vitest'

const mockCreate = vi.fn()

vi.mock('@anthropic-ai/sdk', () => ({
  default: class Anthropic {
    constructor() {
      this.messages = { create: mockCreate }
    }
  },
}))

const { ingestCV } = await import('../../lib/anthropic.mjs')

describe('ingestCV', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('calls the Anthropic client singleton with claude-sonnet-4-6, max_tokens 8000, temperature 0.2', async () => {
    mockCreate.mockResolvedValue({ content: [{ type: 'text', text: 'response' }] })

    const systemBlocks = [{ type: 'text', text: 'system prompt', cache_control: { type: 'ephemeral' } }]
    const userContent = 'Here is my raw CV:\n\nraw text'

    await ingestCV(systemBlocks, userContent)

    expect(mockCreate).toHaveBeenCalledTimes(1)
    const args = mockCreate.mock.calls[0][0]
    expect(args.model).toBe('claude-sonnet-4-6')
    expect(args.max_tokens).toBe(8000)
    expect(args.temperature).toBe(0.2)
    expect(args.system).toBe(systemBlocks)
    expect(args.messages).toEqual([{ role: 'user', content: userContent }])
  })

  it('returns the Anthropic response unchanged', async () => {
    const fakeResponse = { content: [{ type: 'text', text: 'hello' }] }
    mockCreate.mockResolvedValue(fakeResponse)

    const result = await ingestCV([], 'content')

    expect(result).toBe(fakeResponse)
  })
})
