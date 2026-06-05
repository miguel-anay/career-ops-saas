import Anthropic from '@anthropic-ai/sdk'
import 'dotenv/config'

const client = new Anthropic({
  apiKey: process.env.ANTHROPIC_API_KEY,
  timeout: 120_000, // 120 seconds (NFR-03)
})

/**
 * Call Claude to evaluate a job against a user's CV and profile.
 *
 * Uses the 2-block caching structure for prompt caching on system + CV prefix.
 * Model: claude-sonnet-4-6, max_tokens: 8000, temperature: 0.2
 *
 * @param {Array<{type: string, text: string, cache_control: object}>} systemBlocks - System prompt blocks with cache headers
 * @param {string} userContent - The user message content (JD + output contract)
 * @returns {Promise<import('@anthropic-ai/sdk').Message>}
 */
export async function evaluate(systemBlocks, userContent) {
  return client.messages.create({
    model: 'claude-sonnet-4-6',
    max_tokens: 8000,
    temperature: 0.2,
    system: systemBlocks,
    messages: [
      {
        role: 'user',
        content: userContent,
      },
    ],
  })
}
