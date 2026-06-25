import { describe, it, expect, vi } from 'vitest'
import { AnthropicEvaluator } from '../../adapters/AnthropicEvaluator.mjs'

describe('AnthropicEvaluator.evaluate', () => {
  it('builds the prompt, calls evaluate with system blocks and the first message content, and returns the response text', async () => {
    const fakePromptData = {
      system: [{ type: 'text', text: 'system prompt', cache_control: { type: 'ephemeral' } }],
      messages: [{ role: 'user', content: 'Evaluate this job' }],
    }
    const fakeBuildEvaluationPrompt = vi.fn().mockResolvedValue(fakePromptData)
    const fakeAnthropicEvaluate = vi.fn().mockResolvedValue({
      content: [{ type: 'text', text: 'raw evaluation text' }],
    })
    const fakeTenantQuery = vi.fn()

    const evaluator = new AnthropicEvaluator({
      tenantQuery: fakeTenantQuery,
      buildEvaluationPrompt: fakeBuildEvaluationPrompt,
      evaluate: fakeAnthropicEvaluate,
    })

    const result = await evaluator.evaluate('user-1', 'job-1')

    expect(fakeBuildEvaluationPrompt).toHaveBeenCalledWith('user-1', 'job-1', { tenantQuery: fakeTenantQuery })
    expect(fakeAnthropicEvaluate).toHaveBeenCalledWith(fakePromptData.system, 'Evaluate this job')
    expect(result).toBe('raw evaluation text')
  })

  it('returns empty string when the Anthropic response has no content blocks', async () => {
    const fakeBuildEvaluationPrompt = vi.fn().mockResolvedValue({ system: [], messages: [{ role: 'user', content: '' }] })
    const fakeAnthropicEvaluate = vi.fn().mockResolvedValue({ content: [] })
    const fakeTenantQuery = vi.fn()

    const evaluator = new AnthropicEvaluator({
      tenantQuery: fakeTenantQuery,
      buildEvaluationPrompt: fakeBuildEvaluationPrompt,
      evaluate: fakeAnthropicEvaluate,
    })

    const result = await evaluator.evaluate('user-2', 'job-2')

    expect(result).toBe('')
  })
})
