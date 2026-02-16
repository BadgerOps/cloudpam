import { useEffect, useState, useRef } from 'react'
import { Bot, Plus, Trash2, Send, Loader2, AlertCircle, CheckCircle } from 'lucide-react'
import { useAIPlanner } from '../hooks/useAIPlanner'
import { extractPlan } from '../utils/planParser'
import type { GeneratedPlan } from '../api/types'
import { useToast } from '../hooks/useToast'

export default function AIPlannerPage() {
  const {
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
  } = useAIPlanner()

  const [input, setInput] = useState('')
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const { showToast } = useToast()

  useEffect(() => {
    fetchSessions()
  }, [fetchSessions])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [activeSession?.messages, streamingText])

  function handleSend() {
    const msg = input.trim()
    if (!msg || streaming) return
    setInput('')
    sendMessage(msg)
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  async function handleApplyPlan(plan: GeneratedPlan) {
    const res = await applyPlan(plan)
    if (res) {
      showToast(`Created ${res.created} pools`, 'success')
    }
  }

  return (
    <div className="flex h-full">
      {/* Session sidebar */}
      <div className="w-64 border-r border-gray-200 dark:border-gray-700 flex flex-col bg-gray-50 dark:bg-gray-800/50 flex-shrink-0">
        <div className="p-3 border-b border-gray-200 dark:border-gray-700">
          <button
            onClick={() => createSession()}
            className="w-full flex items-center justify-center gap-2 px-3 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 text-sm"
          >
            <Plus className="w-4 h-4" />
            New Session
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-2 space-y-1">
          {sessions.map(session => (
            <div
              key={session.id}
              className={`group flex items-center gap-2 px-3 py-2 rounded-lg cursor-pointer text-sm ${
                activeSession?.id === session.id
                  ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300'
                  : 'hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
              }`}
              onClick={() => selectSession(session.id)}
            >
              <Bot className="w-4 h-4 flex-shrink-0" />
              <span className="truncate flex-1">{session.title}</span>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  deleteSession(session.id)
                }}
                className="opacity-0 group-hover:opacity-100 p-1 hover:text-red-500"
              >
                <Trash2 className="w-3 h-3" />
              </button>
            </div>
          ))}
          {sessions.length === 0 && (
            <p className="text-xs text-gray-400 text-center mt-4">No sessions yet</p>
          )}
        </div>
      </div>

      {/* Chat area */}
      <div className="flex-1 flex flex-col min-w-0">
        {!activeSession ? (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center text-gray-400">
              <Bot className="w-12 h-12 mx-auto mb-3" />
              <p className="text-lg font-medium">AI Network Planner</p>
              <p className="text-sm mt-1">Create a session to start planning your network infrastructure</p>
            </div>
          </div>
        ) : (
          <>
            {/* Header */}
            <div className="px-4 py-3 border-b border-gray-200 dark:border-gray-700">
              <h2 className="font-medium text-gray-900 dark:text-gray-100">{activeSession.title}</h2>
            </div>

            {/* Messages */}
            <div className="flex-1 overflow-y-auto p-4 space-y-4">
              {loading && (
                <div className="flex justify-center py-8">
                  <Loader2 className="w-6 h-6 animate-spin text-gray-400" />
                </div>
              )}

              {activeSession.messages.map(msg => (
                <MessageBubble
                  key={msg.id}
                  role={msg.role}
                  content={msg.content}
                  onApplyPlan={handleApplyPlan}
                />
              ))}

              {streaming && streamingText && (
                <MessageBubble
                  role="assistant"
                  content={streamingText}
                  isStreaming
                  onApplyPlan={handleApplyPlan}
                />
              )}

              {streaming && !streamingText && (
                <div className="flex items-center gap-2 text-gray-400 text-sm">
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Thinking...
                </div>
              )}

              {error && (
                <div className="flex items-center gap-2 text-red-500 text-sm bg-red-50 dark:bg-red-900/20 px-3 py-2 rounded-lg">
                  <AlertCircle className="w-4 h-4 flex-shrink-0" />
                  {error}
                  <button onClick={() => setError(null)} className="ml-auto text-xs underline">dismiss</button>
                </div>
              )}

              <div ref={messagesEndRef} />
            </div>

            {/* Input */}
            <div className="p-4 border-t border-gray-200 dark:border-gray-700">
              <div className="flex gap-2">
                <textarea
                  value={input}
                  onChange={e => setInput(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder="Describe the network you want to plan..."
                  rows={1}
                  className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 resize-none focus:outline-none focus:ring-2 focus:ring-blue-500"
                  disabled={streaming}
                />
                <button
                  onClick={handleSend}
                  disabled={streaming || !input.trim()}
                  className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {streaming ? <Loader2 className="w-5 h-5 animate-spin" /> : <Send className="w-5 h-5" />}
                </button>
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

interface MessageBubbleProps {
  role: string
  content: string
  isStreaming?: boolean
  onApplyPlan: (plan: GeneratedPlan) => void
}

function MessageBubble({ role, content, isStreaming, onApplyPlan }: MessageBubbleProps) {
  const [applying, setApplying] = useState(false)

  const isUser = role === 'user'
  const plan = !isStreaming ? extractPlan(content) : null

  async function handleApply() {
    if (!plan || applying) return
    setApplying(true)
    onApplyPlan(plan)
    // The parent will handle the result; we just show "applying" state
    setApplying(false)
  }

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div
        className={`max-w-[80%] px-4 py-3 rounded-lg ${
          isUser
            ? 'bg-blue-600 text-white'
            : 'bg-gray-100 dark:bg-gray-800 text-gray-900 dark:text-gray-100'
        }`}
      >
        <div className="whitespace-pre-wrap text-sm break-words">{content}</div>

        {plan && (
          <div className="mt-3 border border-gray-300 dark:border-gray-600 rounded-lg p-3 bg-white dark:bg-gray-900">
            <div className="flex items-center justify-between mb-2">
              <span className="font-medium text-sm text-gray-900 dark:text-gray-100">{plan.name || 'Generated Plan'}</span>
              <span className="text-xs text-gray-500">{plan.pools.length} pool{plan.pools.length !== 1 ? 's' : ''}</span>
            </div>
            <div className="space-y-1 mb-3">
              {plan.pools.map((p, i) => (
                <div key={i} className="text-xs font-mono text-gray-600 dark:text-gray-400 flex gap-2">
                  <span>{p.parent_ref ? '  \u2514\u2500 ' : ''}{p.name}</span>
                  <span className="text-gray-400">{p.cidr}</span>
                  <span className="text-gray-500">[{p.type}]</span>
                </div>
              ))}
            </div>
            <button
              onClick={handleApply}
              disabled={applying}
              className="w-full flex items-center justify-center gap-2 px-3 py-1.5 bg-green-600 text-white rounded text-sm hover:bg-green-700 disabled:opacity-50"
            >
              {applying ? (
                <><Loader2 className="w-4 h-4 animate-spin" /> Applying...</>
              ) : (
                <><CheckCircle className="w-4 h-4" /> Apply Plan</>
              )}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
