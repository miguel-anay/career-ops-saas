// Generic OpenAI-compatible evaluator client.
//
// Almost every LLM host (Qwen/DashScope, DeepSeek, MiniMax, OpenAI, ...) exposes
// the same `/chat/completions` contract, so a single client covers all of them —
// only baseURL + key + model change, and those are CONFIG, not code. Adding a new
// provider = a preset entry (or LLM_BASE_URL override) + env vars. No new file.
//
// ponytail: raw fetch instead of the `openai` SDK — the contract is stable and
// Node 20 has global fetch, so a dependency buys nothing.

// provider → { baseURL, default model }. LLM_BASE_URL / LLM_MODEL override these.
const PRESETS = {
  qwen: { baseURL: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1', model: 'qwen-plus' },
  deepseek: { baseURL: 'https://api.deepseek.com/v1', model: 'deepseek-chat' },
  minimax: { baseURL: 'https://api.minimaxi.chat/v1', model: 'MiniMax-Text-01' },
  openai: { baseURL: 'https://api.openai.com/v1', model: 'gpt-4o-mini' },
}

function flattenSystem(systemBlocks) {
  // Anthropic-style system blocks ({type,text,cache_control}) → one plain string.
  // OpenAI-compatible chat has no cache_control and wants a single system message.
  if (Array.isArray(systemBlocks)) return systemBlocks.map((b) => b?.text || '').join('\n\n')
  return String(systemBlocks || '')
}

/**
 * Evaluate a job against a CV via an OpenAI-compatible chat endpoint.
 *
 * Provider is selected by `EVALUATOR` (qwen | deepseek | minimax | openai), with
 * `LLM_BASE_URL` / `LLM_MODEL` overrides and `LLM_API_KEY` for auth. Same limits
 * as the Anthropic path: max_tokens 8000, temperature 0.2, 120s timeout (NFR-03).
 *
 * @param {Array<{type:string,text:string}>|string} systemBlocks
 * @param {string} userContent - The user message content (JD + output contract)
 * @returns {Promise<{choices: Array<{message: {content: string}}>}>}
 */
/**
 * Ingest a raw CV via an OpenAI-compatible chat endpoint.
 *
 * Same provider selection and config as evaluate(); normalises the response
 * shape to match Anthropic's { content: [{ type: 'text', text }] } contract
 * so ingest-cv.mjs can use either provider without branching on response shape.
 *
 * @param {Array<{type:string,text:string}>|string} systemBlocks
 * @param {string} userContent - The user message content (raw CV + output contract)
 * @returns {Promise<{content: Array<{type: string, text: string}>}>}
 */
export async function ingestCV(systemBlocks, userContent) {
  const response = await evaluate(systemBlocks, userContent)
  const text = response.choices?.[0]?.message?.content || ''
  return { content: [{ type: 'text', text }] }
}

export async function evaluate(systemBlocks, userContent) {
  const provider = process.env.EVALUATOR || 'qwen'
  const preset = PRESETS[provider]
  const baseURL = process.env.LLM_BASE_URL || preset?.baseURL
  const model = process.env.LLM_MODEL || preset?.model
  const apiKey = process.env.LLM_API_KEY

  if (!baseURL) throw new Error(`openai-compat: unknown provider "${provider}" (set LLM_BASE_URL)`)
  if (!model) throw new Error(`openai-compat: no model for "${provider}" (set LLM_MODEL)`)
  if (!apiKey) throw new Error('openai-compat: LLM_API_KEY is not set')

  const res = await fetch(`${baseURL}/chat/completions`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${apiKey}`,
    },
    body: JSON.stringify({
      model,
      temperature: 0.2,
      max_tokens: 8000,
      messages: [
        { role: 'system', content: flattenSystem(systemBlocks) },
        { role: 'user', content: userContent },
      ],
    }),
    signal: AbortSignal.timeout(120_000), // NFR-03
  })

  if (!res.ok) {
    const detail = await res.text().catch(() => '')
    throw new Error(`openai-compat: request failed ${res.status} ${detail.slice(0, 500)}`)
  }
  return res.json()
}
