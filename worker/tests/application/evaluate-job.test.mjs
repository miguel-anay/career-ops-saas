import { describe, it, expect, vi } from 'vitest'
import { EvaluateJob } from '../../application/EvaluateJob.mjs'

const VALID_RESPONSE_TEXT = `## Block A — Role & Company Fit
Score: 4.2/5
Strong alignment.

## Block B — Technical Match
Score: 4.5/5
All required skills present.

**Overall Score: 4.1/5**`

describe('EvaluateJob.run', () => {
  it('happy path: passes an Evaluation built from the evaluator raw text to repository.save', async () => {
    const fakeEvaluator = { evaluate: vi.fn().mockResolvedValue(VALID_RESPONSE_TEXT) }
    const fakeRepository = { save: vi.fn().mockResolvedValue(undefined) }

    const useCase = new EvaluateJob({ evaluator: fakeEvaluator, repository: fakeRepository })

    await useCase.run({ userId: 'user-1', jobId: 'job-1' })

    expect(fakeEvaluator.evaluate).toHaveBeenCalledWith('user-1', 'job-1')
    expect(fakeRepository.save).toHaveBeenCalledTimes(1)

    const [userIdArg, jobIdArg, evaluationArg] = fakeRepository.save.mock.calls[0]
    expect(userIdArg).toBe('user-1')
    expect(jobIdArg).toBe('job-1')
    expect(evaluationArg.isParseError).toBe(false)
    expect(evaluationArg.score).toBe(4.1)
    expect(evaluationArg.contentMd).toBe(VALID_RESPONSE_TEXT)
    expect(evaluationArg.blocks.blockA.score).toBe(4.2)
    expect(evaluationArg.statusNote).toBeNull()
  })

  it('parse-error path: unparseable evaluator text still produces a persistable Evaluation passed to repository.save', async () => {
    const garbledText = 'This is not a valid block format at all.'
    const fakeEvaluator = { evaluate: vi.fn().mockResolvedValue(garbledText) }
    const fakeRepository = { save: vi.fn().mockResolvedValue(undefined) }

    const useCase = new EvaluateJob({ evaluator: fakeEvaluator, repository: fakeRepository })

    await useCase.run({ userId: 'user-2', jobId: 'job-2' })

    expect(fakeRepository.save).toHaveBeenCalledTimes(1)
    const [, , evaluationArg] = fakeRepository.save.mock.calls[0]
    expect(evaluationArg.isParseError).toBe(true)
    expect(evaluationArg.score).toBeNull()
    expect(evaluationArg.blocks.parse_error).toBe(true)
    expect(evaluationArg.statusNote).toBe('Evaluation completed (parse error in blocks)')
  })
})
