import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { evaluate, ingestCV } from '../../lib/openai-compat.mjs'

describe('openai-compat evaluate', () => {
  const realFetch = global.fetch

  beforeEach(() => {
    process.env.EVALUATOR = 'qwen'
    process.env.LLM_API_KEY = 'test-key'
    process.env.LLM_MODEL = 'qwen-plus'
    delete process.env.LLM_BASE_URL
  })

  afterEach(() => {
    global.fetch = realFetch
    vi.restoreAllMocks()
    delete process.env.EVALUATOR
    delete process.env.LLM_API_KEY
    delete process.env.LLM_MODEL
    delete process.env.LLM_BASE_URL
  })

  it('posts flattened system + user to the provider preset URL and returns parsed JSON', async () => {
    const fakeJson = { choices: [{ message: { content: 'raw eval' } }] }
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => fakeJson })
    global.fetch = fetchMock

    const system = [
      { type: 'text', text: 'SYS A', cache_control: { type: 'ephemeral' } },
      { type: 'text', text: 'CV B', cache_control: { type: 'ephemeral' } },
    ]
    const result = await evaluate(system, 'JOB DESC')

    expect(result).toEqual(fakeJson)
    expect(fetchMock).toHaveBeenCalledTimes(1)

    const [url, opts] = fetchMock.mock.calls[0]
    expect(url).toBe('https://dashscope-intl.aliyuncs.com/compatible-mode/v1/chat/completions')
    expect(opts.method).toBe('POST')
    expect(opts.headers.Authorization).toBe('Bearer test-key')

    const body = JSON.parse(opts.body)
    expect(body.model).toBe('qwen-plus')
    // system blocks (cache_control dropped) flattened into one plain string
    expect(body.messages[0]).toEqual({ role: 'system', content: 'SYS A\n\nCV B' })
    expect(body.messages[1]).toEqual({ role: 'user', content: 'JOB DESC' })
  })

  it('throws when the response is not ok', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 429, text: async () => 'rate limited' })
    await expect(evaluate([], 'x')).rejects.toThrow(/429/)
  })

  it('throws when LLM_API_KEY is missing', async () => {
    delete process.env.LLM_API_KEY
    await expect(evaluate([], 'x')).rejects.toThrow(/LLM_API_KEY/)
  })

  it('throws on an unknown provider with no base URL override', async () => {
    process.env.EVALUATOR = 'nonsense'
    await expect(evaluate([], 'x')).rejects.toThrow(/unknown provider/)
  })
})

describe('openai-compat ingestCV', () => {
  const realFetch = global.fetch

  beforeEach(() => {
    process.env.EVALUATOR = 'qwen'
    process.env.LLM_API_KEY = 'test-key'
    process.env.LLM_MODEL = 'qwen-plus'
    delete process.env.LLM_BASE_URL
  })

  afterEach(() => {
    global.fetch = realFetch
    vi.restoreAllMocks()
    delete process.env.EVALUATOR
    delete process.env.LLM_API_KEY
    delete process.env.LLM_MODEL
    delete process.env.LLM_BASE_URL
  })

  it('normalises the OpenAI response to Anthropic shape { content: [{ type: text, text }] }', async () => {
    const fakeOpenAIResponse = { choices: [{ message: { content: '===CV_MARKDOWN===\n# Jane\n\n===PROFILE_JSON===\n```json\n{}\n```' } }] }
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => fakeOpenAIResponse })

    const result = await ingestCV([{ type: 'text', text: 'system' }], 'raw cv')

    expect(result).toEqual({
      content: [{ type: 'text', text: '===CV_MARKDOWN===\n# Jane\n\n===PROFILE_JSON===\n```json\n{}\n```' }],
    })
  })

  it('returns empty text when choices are missing', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })

    const result = await ingestCV([], 'x')

    expect(result).toEqual({ content: [{ type: 'text', text: '' }] })
  })
})
