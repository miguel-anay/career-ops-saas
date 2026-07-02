import { describe, it, expect, vi } from 'vitest'
import { LlmEvaluator } from '../../adapters/LlmEvaluator.mjs'

describe('LlmEvaluator.evaluate', () => {
  const fakePromptData = {
    system: [{ type: 'text', text: 'system prompt', cache_control: { type: 'ephemeral' } }],
    messages: [{ role: 'user', content: 'Evaluate this job' }],
  }

  it('builds the prompt, calls evaluate with system blocks + first message, and extracts Anthropic-shaped text', async () => {
    const buildEvaluationPrompt = vi.fn().mockResolvedValue(fakePromptData)
    const evaluate = vi.fn().mockResolvedValue({ content: [{ type: 'text', text: 'anthropic text' }] })
    const tenantQuery = vi.fn()
    const ev = new LlmEvaluator({ tenantQuery, buildEvaluationPrompt, evaluate })

    const result = await ev.evaluate('user-1', 'job-1')

    expect(buildEvaluationPrompt).toHaveBeenCalledWith('user-1', 'job-1', { tenantQuery })
    expect(evaluate).toHaveBeenCalledWith(fakePromptData.system, 'Evaluate this job')
    expect(result).toBe('anthropic text')
  })

  it('extracts OpenAI-compatible text (choices[0].message.content)', async () => {
    const buildEvaluationPrompt = vi.fn().mockResolvedValue(fakePromptData)
    const evaluate = vi.fn().mockResolvedValue({ choices: [{ message: { content: 'openai text' } }] })
    const ev = new LlmEvaluator({ tenantQuery: vi.fn(), buildEvaluationPrompt, evaluate })

    expect(await ev.evaluate('u', 'j')).toBe('openai text')
  })

  it('returns empty string when the response has neither shape', async () => {
    const buildEvaluationPrompt = vi.fn().mockResolvedValue({ system: [], messages: [{ role: 'user', content: '' }] })
    const evaluate = vi.fn().mockResolvedValue({})
    const ev = new LlmEvaluator({ tenantQuery: vi.fn(), buildEvaluationPrompt, evaluate })

    expect(await ev.evaluate('u', 'j')).toBe('')
  })
})
