import { useState, useCallback, useRef } from 'react'
import { get, post, del, streamPost } from '../api/client'
import type {
  Conversation,
  ConversationWithMessages,
  ConversationsListResponse,
  ApplyPlanResponse,
  GeneratedPlan,
} from '../api/types'

export function useAIPlanner() {
  const [sessions, setSessions] = useState<Conversation[]>([])
  const [activeSession, setActiveSession] = useState<ConversationWithMessages | null>(null)
  const [streaming, setStreaming] = useState(false)
  const [streamingText, setStreamingText] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const streamingTextRef = useRef('')

  const fetchSessions = useCallback(async () => {
    try {
      const res = await get<ConversationsListResponse>('/api/v1/ai/sessions')
      setSessions(res.items || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load sessions')
    }
  }, [])

  const createSession = useCallback(async (title?: string) => {
    try {
      const conv = await post<Conversation>('/api/v1/ai/sessions', { title: title || 'New Planning Session' })
      setSessions(prev => [conv, ...prev])
      setActiveSession({ ...conv, messages: [] })
      return conv
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create session')
      return null
    }
  }, [])

  const selectSession = useCallback(async (id: string) => {
    setLoading(true)
    try {
      const conv = await get<ConversationWithMessages>(`/api/v1/ai/sessions/${id}`)
      setActiveSession(conv)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load session')
    } finally {
      setLoading(false)
    }
  }, [])

  const deleteSession = useCallback(async (id: string) => {
    try {
      await del(`/api/v1/ai/sessions/${id}`)
      setSessions(prev => prev.filter(s => s.id !== id))
      if (activeSession?.id === id) {
        setActiveSession(null)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete session')
    }
  }, [activeSession])

  const sendMessage = useCallback(async (message: string) => {
    if (!activeSession || streaming) return

    // Add user message to the UI immediately
    const userMsg = {
      id: crypto.randomUUID(),
      conversation_id: activeSession.id,
      role: 'user' as const,
      content: message,
      created_at: new Date().toISOString(),
    }
    setActiveSession(prev =>
      prev ? { ...prev, messages: [...prev.messages, userMsg] } : null
    )

    setStreaming(true)
    setStreamingText('')
    streamingTextRef.current = ''
    setError(null)

    await streamPost('/api/v1/ai/chat', {
      session_id: activeSession.id,
      message,
    }, {
      onDelta: (text) => {
        streamingTextRef.current += text
        setStreamingText(streamingTextRef.current)
      },
      onDone: () => {
        // Add the assistant message
        const assistantMsg = {
          id: crypto.randomUUID(),
          conversation_id: activeSession.id,
          role: 'assistant' as const,
          content: streamingTextRef.current,
          created_at: new Date().toISOString(),
        }
        setActiveSession(prev =>
          prev ? { ...prev, messages: [...prev.messages, assistantMsg] } : null
        )
        setStreamingText('')
        streamingTextRef.current = ''
        setStreaming(false)
      },
      onError: (err) => {
        setError(err.message)
        setStreaming(false)
        setStreamingText('')
        streamingTextRef.current = ''
      },
    })
  }, [activeSession, streaming])

  const applyPlan = useCallback(async (plan: GeneratedPlan) => {
    if (!activeSession) return null
    try {
      const res = await post<ApplyPlanResponse>(
        `/api/v1/ai/sessions/${activeSession.id}/apply-plan`,
        { plan, skip_conflicts: false },
      )
      return res
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to apply plan')
      return null
    }
  }, [activeSession])

  return {
    sessions,
    activeSession,
    streaming,
    streamingText,
    loading,
    error,
    fetchSessions,
    createSession,
    selectSession,
    deleteSession,
    sendMessage,
    applyPlan,
    setError,
  }
}
