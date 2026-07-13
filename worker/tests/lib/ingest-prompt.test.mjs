import { describe, it, expect } from 'vitest'
import {
  buildIngestPrompt,
  INGEST_SYSTEM_PROMPT,
  INGEST_MERGE_SYSTEM_PROMPT,
} from '../../lib/ingest-prompt.mjs'

describe('buildIngestPrompt', () => {
  it('returns a { system, messages } shape', () => {
    const result = buildIngestPrompt('Jane Doe — Senior Engineer')

    expect(result).toHaveProperty('system')
    expect(result).toHaveProperty('messages')
    expect(Array.isArray(result.system)).toBe(true)
    expect(Array.isArray(result.messages)).toBe(true)
  })

  it('system block carries cache_control: ephemeral for prompt caching', () => {
    const result = buildIngestPrompt('raw cv text')

    expect(result.system.length).toBeGreaterThan(0)
    for (const block of result.system) {
      expect(block).toHaveProperty('cache_control')
      expect(block.cache_control).toEqual({ type: 'ephemeral' })
      expect(block.type).toBe('text')
    }
  })

  it('system block text matches the exported INGEST_SYSTEM_PROMPT contract', () => {
    const result = buildIngestPrompt('raw cv text')

    expect(result.system[0].text).toBe(INGEST_SYSTEM_PROMPT)
    expect(INGEST_SYSTEM_PROMPT).toContain('===CV_MARKDOWN===')
    expect(INGEST_SYSTEM_PROMPT).toContain('===PROFILE_JSON===')
  })

  it('user message content contains the raw CV text', () => {
    const rawCV = 'Jane Doe, Senior Engineer, 10 years experience'
    const result = buildIngestPrompt(rawCV)

    expect(result.messages).toHaveLength(1)
    expect(result.messages[0].role).toBe('user')
    expect(result.messages[0].content).toContain(rawCV)
  })

  it('with no existingCvMarkdown, behaves exactly as today (INGEST_SYSTEM_PROMPT, raw-only user message)', () => {
    const rawCV = 'Jane Doe, Senior Engineer, 10 years experience'
    const result = buildIngestPrompt(rawCV, '')

    expect(result.system[0].text).toBe(INGEST_SYSTEM_PROMPT)
    expect(result.messages[0].content).toBe(`Here is my raw CV:\n\n${rawCV}`)
  })

  describe('with a non-empty existingCvMarkdown (merge variant)', () => {
    const rawCV = 'NEW: Staff Engineer at Acme, 2024-present'
    const existingCvMarkdown = '# Jane Doe\n## Experience\n- Senior Engineer at Acme, 2020-2024'

    it('returns the INGEST_MERGE_SYSTEM_PROMPT system block', () => {
      const result = buildIngestPrompt(rawCV, existingCvMarkdown)

      expect(result.system[0].text).toBe(INGEST_MERGE_SYSTEM_PROMPT)
      expect(INGEST_MERGE_SYSTEM_PROMPT).toContain('===CV_MARKDOWN===')
      expect(INGEST_MERGE_SYSTEM_PROMPT).toContain('===PROFILE_JSON===')
      expect(INGEST_MERGE_SYSTEM_PROMPT).not.toBe(INGEST_SYSTEM_PROMPT)
    })

    it('still carries cache_control: ephemeral on the merge system block', () => {
      const result = buildIngestPrompt(rawCV, existingCvMarkdown)

      expect(result.system[0].cache_control).toEqual({ type: 'ephemeral' })
      expect(result.system[0].type).toBe('text')
    })

    it('user message contains both the existing CV and the new text, labeled distinctly', () => {
      const result = buildIngestPrompt(rawCV, existingCvMarkdown)

      expect(result.messages).toHaveLength(1)
      expect(result.messages[0].role).toBe('user')
      expect(result.messages[0].content).toContain(existingCvMarkdown)
      expect(result.messages[0].content).toContain(rawCV)
      expect(result.messages[0].content).toMatch(/existing/i)
      expect(result.messages[0].content).toMatch(/new/i)
    })
  })
})
