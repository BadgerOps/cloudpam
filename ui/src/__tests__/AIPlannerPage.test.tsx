import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AIPlannerPage from '../pages/AIPlannerPage'
import type { ApplyPlanResponse } from '../api/types'

const mockUseAIPlanner = vi.fn()
const mockUseToast = vi.fn()

vi.mock('../hooks/useAIPlanner', () => ({
  useAIPlanner: () => mockUseAIPlanner(),
}))

vi.mock('../hooks/useToast', () => ({
  useToast: () => mockUseToast(),
}))

const planMessage = `Here is a plan:

\`\`\`json
{
  "name": "Production VPC",
  "description": "Production network layout",
  "pools": [
    {"ref": "root", "name": "prod-vpc", "cidr": "10.0.0.0/16", "type": "supernet"}
  ]
}
\`\`\`
`

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => {
    resolve = res
  })
  return { promise, resolve }
}

describe('AIPlannerPage', () => {
  const showToast = vi.fn()
  const applyPlan = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()

    // jsdom does not implement scrollIntoView
    Element.prototype.scrollIntoView = vi.fn()

    mockUseToast.mockReturnValue({ showToast })
    mockUseAIPlanner.mockReturnValue({
      sessions: [{ id: 'a', title: 'Session A', created_at: '', updated_at: '' }],
      activeSession: {
        id: 'a',
        title: 'Session A',
        created_at: '',
        updated_at: '',
        messages: [
          { id: 'm1', conversation_id: 'a', role: 'assistant', content: planMessage, created_at: '' },
        ],
      },
      streaming: false,
      streamingText: '',
      loading: false,
      applying: false,
      error: null,
      fetchSessions: vi.fn(),
      createSession: vi.fn(),
      selectSession: vi.fn(),
      deleteSession: vi.fn(),
      sendMessage: vi.fn(),
      applyPlan,
      setError: vi.fn(),
    })
  })

  it('applies a generated plan once and reports the result', async () => {
    applyPlan.mockResolvedValue({ created: 1, skipped: 0, errors: [], root_pool_id: 1, pool_map: {} })

    render(<AIPlannerPage />)

    fireEvent.click(screen.getByRole('button', { name: /Apply Plan/i }))

    await waitFor(() => {
      expect(applyPlan).toHaveBeenCalledTimes(1)
      expect(showToast).toHaveBeenCalledWith('Created 1 pools', 'success')
    })
  })

  it('does not submit a duplicate apply while the first one is in flight', async () => {
    const pending = deferred<ApplyPlanResponse>()
    applyPlan.mockReturnValue(pending.promise)

    render(<AIPlannerPage />)

    const button = screen.getByRole('button', { name: /Apply Plan/i })
    fireEvent.click(button)
    fireEvent.click(button)

    expect(applyPlan).toHaveBeenCalledTimes(1)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Applying/i })).toHaveProperty('disabled', true)
    })

    fireEvent.click(screen.getByRole('button', { name: /Applying/i }))
    expect(applyPlan).toHaveBeenCalledTimes(1)

    await act(async () => {
      pending.resolve({ created: 1, skipped: 0, errors: [], root_pool_id: 1, pool_map: {} })
      await pending.promise
    })

    // The button re-enables once the request settles
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Apply Plan/i })).toHaveProperty('disabled', false)
    })
  })

  it('re-enables the apply button after a failed apply', async () => {
    applyPlan.mockRejectedValue(new Error('boom'))

    render(<AIPlannerPage />)

    fireEvent.click(screen.getByRole('button', { name: /Apply Plan/i }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Apply Plan/i })).toHaveProperty('disabled', false)
    })
    expect(applyPlan).toHaveBeenCalledTimes(1)
  })
})
