import { useState, useCallback, useRef } from 'react'
import { get, post, del, streamPost } from '../api/client'
import { useLatestRequest } from './useLatestRequest'
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
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const streamingTextRef = useRef('')
  const applyingRef = useRef(false)
  // Guards the active session against out-of-order session loads
  const { begin: beginActive, isCurrent: isCurrentActive } = useLatestRequest()
  // Session currently shown; a stream started for another session is dropped
  const activeSessionIdRef = useRef<string | null>(null)

  const focusSession = useCallback((id: string | null) => {
    if (activeSessionIdRef.current === id) return
    activeSessionIdRef.current = id
    // Whatever was streaming belonged to the previous session
    setStreaming(false)
    setStreamingText('')
    streamingTextRef.current = ''
  }, [])

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
      beginActive()
      focusSession(conv.id)
      setActiveSession({ ...conv, messages: [] })
      return conv
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create session')
      return null
    }
  }, [beginActive, focusSession])

  const selectSession = useCallback(async (id: string) => {
    const token = beginActive()
    setLoading(true)
    try {
      const conv = await get<ConversationWithMessages>(`/api/v1/ai/sessions/${id}`)
      if (!isCurrentActive(token)) return
      focusSession(conv.id)
      setActiveSession(conv)
      setError(null)
    } catch (err) {
      if (!isCurrentActive(token)) return
      setError(err instanceof Error ? err.message : 'Failed to load session')
    } finally {
      setLoading(false)
    }
  }, [beginActive, isCurrentActive, focusSession])

  const deleteSession = useCallback(async (id: string) => {
    try {
      await del(`/api/v1/ai/sessions/${id}`)
      setSessions(prev => prev.filter(s => s.id !== id))
      if (activeSession?.id === id) {
        beginActive()
        focusSession(null)
        setActiveSession(null)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete session')
    }
  }, [activeSession, beginActive, focusSession])

  const sendMessage = useCallback(async (message: string) => {
    if (!activeSession || streaming) return

    // The session this stream belongs to; if the user switches sessions
    // mid-stream, the remaining chunks must not land on the new session.
    const sessionId = activeSession.id
    activeSessionIdRef.current = sessionId
    const isStreamSessionActive = () => activeSessionIdRef.current === sessionId

    // Add user message to the UI immediately
    const userMsg = {
      id: crypto.randomUUID(),
      conversation_id: sessionId,
      role: 'user' as const,
      content: message,
      created_at: new Date().toISOString(),
    }
    setActiveSession(prev =>
      prev && prev.id === sessionId
        ? { ...prev, messages: [...prev.messages, userMsg] }
        : prev
    )

    setStreaming(true)
    setStreamingText('')
    streamingTextRef.current = ''
    setError(null)

    await streamPost('/api/v1/ai/chat', {
      session_id: sessionId,
      message,
    }, {
      onDelta: (text) => {
        if (!isStreamSessionActive()) return
        streamingTextRef.current += text
        setStreamingText(streamingTextRef.current)
      },
      onDone: () => {
        if (!isStreamSessionActive()) return
        // Add the assistant message
        const assistantMsg = {
          id: crypto.randomUUID(),
          conversation_id: sessionId,
          role: 'assistant' as const,
          content: streamingTextRef.current,
          created_at: new Date().toISOString(),
        }
        setActiveSession(prev =>
          prev && prev.id === sessionId
            ? { ...prev, messages: [...prev.messages, assistantMsg] }
            : prev
        )
        setStreamingText('')
        streamingTextRef.current = ''
        setStreaming(false)
      },
      onError: (err) => {
        if (!isStreamSessionActive()) return
        setError(err.message)
        setStreaming(false)
        setStreamingText('')
        streamingTextRef.current = ''
      },
    })
  }, [activeSession, streaming])

  const applyPlan = useCallback(async (plan: GeneratedPlan) => {
    if (!activeSession || applyingRef.current) return null
    // A duplicate apply would create duplicate pools, so gate it on a ref that
    // updates synchronously rather than on the re-rendered `applying` state.
    applyingRef.current = true
    setApplying(true)
    try {
      const res = await post<ApplyPlanResponse>(
        `/api/v1/ai/sessions/${activeSession.id}/apply-plan`,
        { plan, skip_conflicts: false },
      )
      return res
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to apply plan')
      return null
    } finally {
      applyingRef.current = false
      setApplying(false)
    }
  }, [activeSession])

  return {
    sessions,
    activeSession,
    streaming,
    streamingText,
    loading,
    applying,
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
