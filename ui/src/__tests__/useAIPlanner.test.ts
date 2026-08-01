import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAIPlanner } from '../hooks/useAIPlanner'
import { get, post, streamPost, type SSECallbacks } from '../api/client'
import type { ApplyPlanResponse, ConversationWithMessages, GeneratedPlan } from '../api/types'

vi.mock('../api/client', () => ({
  get: vi.fn(),
  post: vi.fn(),
  del: vi.fn(),
  streamPost: vi.fn(),
}))

const mockGet = vi.mocked(get)
const mockPost = vi.mocked(post)
const mockStreamPost = vi.mocked(streamPost)

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => {
    resolve = res
  })
  return { promise, resolve }
}

function conversation(id: string): ConversationWithMessages {
  return {
    id,
    title: `Session ${id}`,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    messages: [],
  }
}

const plan: GeneratedPlan = { name: 'Plan', description: '', pools: [] }

describe('useAIPlanner', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('drops stream output for a session the user has switched away from', async () => {
    mockGet
      .mockResolvedValueOnce(conversation('a') as never)
      .mockResolvedValueOnce(conversation('b') as never)

    let streamCallbacks: SSECallbacks | undefined
    mockStreamPost.mockImplementation((_path, _data, callbacks) => {
      streamCallbacks = callbacks
      return new Promise<void>(() => {
        // stays pending for the duration of the test
      })
    })

    const { result } = renderHook(() => useAIPlanner())

    await act(async () => {
      await result.current.selectSession('a')
    })
    expect(result.current.activeSession?.id).toBe('a')

    act(() => {
      void result.current.sendMessage('plan a /16')
    })
    await waitFor(() => expect(mockStreamPost).toHaveBeenCalledTimes(1))

    // User switches to another session while the answer is still streaming
    await act(async () => {
      await result.current.selectSession('b')
    })
    expect(result.current.activeSession?.id).toBe('b')

    act(() => {
      streamCallbacks?.onDelta('half an answer')
    })
    expect(result.current.streamingText).toBe('')

    act(() => {
      streamCallbacks?.onDone()
    })

    expect(result.current.activeSession?.id).toBe('b')
    expect(result.current.activeSession?.messages).toHaveLength(0)
    expect(result.current.streamingText).toBe('')
    expect(result.current.streaming).toBe(false)
  })

  it('still appends the answer when the session has not changed', async () => {
    mockGet.mockResolvedValueOnce(conversation('a') as never)

    let streamCallbacks: SSECallbacks | undefined
    mockStreamPost.mockImplementation((_path, _data, callbacks) => {
      streamCallbacks = callbacks
      return new Promise<void>(() => {})
    })

    const { result } = renderHook(() => useAIPlanner())

    await act(async () => {
      await result.current.selectSession('a')
    })

    act(() => {
      void result.current.sendMessage('plan a /16')
    })
    await waitFor(() => expect(mockStreamPost).toHaveBeenCalledTimes(1))

    act(() => {
      streamCallbacks?.onDelta('an answer')
      streamCallbacks?.onDone()
    })

    const roles = result.current.activeSession?.messages.map(m => m.role)
    expect(roles).toEqual(['user', 'assistant'])
    expect(result.current.activeSession?.messages[1].content).toBe('an answer')
  })

  it('ignores a second applyPlan while the first is still in flight', async () => {
    mockGet.mockResolvedValueOnce(conversation('a') as never)

    const apply = deferred<ApplyPlanResponse>()
    mockPost.mockReturnValueOnce(apply.promise as Promise<never>)

    const { result } = renderHook(() => useAIPlanner())

    await act(async () => {
      await result.current.selectSession('a')
    })

    let secondResult: ApplyPlanResponse | null = null
    act(() => {
      void result.current.applyPlan(plan)
      void result.current.applyPlan(plan).then(res => {
        secondResult = res
      })
    })

    expect(mockPost).toHaveBeenCalledTimes(1)
    expect(result.current.applying).toBe(true)

    await act(async () => {
      apply.resolve({ created: 2, skipped: 0, errors: [], root_pool_id: 1, pool_map: {} })
      await apply.promise
    })

    expect(secondResult).toBeNull()
    expect(result.current.applying).toBe(false)
  })
})
