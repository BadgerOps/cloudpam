import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { KeyRound, Eye, EyeOff, Server, User } from 'lucide-react'
import { useAuth } from '../hooks/useAuth'

type Tab = 'password' | 'apikey'

export default function LoginPage() {
  const { loginWithPassword, loginWithApiKey, isAuthenticated, localAuthEnabled, authChecked } = useAuth()
  const navigate = useNavigate()

  const [tab, setTab] = useState<Tab>('apikey')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [showSecret, setShowSecret] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  // Switch to password tab once we know local auth is available
  useEffect(() => {
    if (authChecked && localAuthEnabled) {
      setTab('password')
    }
  }, [authChecked, localAuthEnabled])

  // Redirect if already authenticated
  useEffect(() => {
    if (isAuthenticated) {
      navigate('/', { replace: true })
    }
  }, [isAuthenticated, navigate])

  async function handlePasswordSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!username.trim() || !password) return
    setLoading(true)
    setError('')
    try {
      await loginWithPassword(username.trim(), password)
      navigate('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed')
    } finally {
      setLoading(false)
    }
  }

  async function handleApiKeySubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!apiKey.trim()) return
    setLoading(true)
    setError('')
    try {
      await loginWithApiKey(apiKey.trim())
      navigate('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed')
    } finally {
      setLoading(false)
    }
  }

  // Don't render until we know the auth configuration
  if (!authChecked) {
    return (
      <div className="min-h-screen bg-gray-50 dark:bg-gray-900 flex items-center justify-center">
        <div className="text-gray-400 text-sm">Loading...</div>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-900 flex items-center justify-center">
      <div className="w-full max-w-md">
        <div className="text-center mb-8">
          <div className="inline-flex p-3 bg-blue-600 rounded-xl mb-4">
            <Server className="w-8 h-8 text-white" />
          </div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">CloudPAM</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">IP Address Management</p>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-xl shadow-lg p-6">
          {/* Tab bar — shown when local auth is available */}
          {localAuthEnabled && (
            <div className="flex border-b border-gray-200 dark:border-gray-700 mb-4">
              <button
                type="button"
                onClick={() => { setTab('password'); setError('') }}
                className={`flex-1 py-2 text-sm font-medium border-b-2 transition-colors ${
                  tab === 'password'
                    ? 'border-blue-600 text-blue-600 dark:text-blue-400'
                    : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400'
                }`}
              >
                <User className="w-4 h-4 inline mr-1.5" />
                Sign In
              </button>
              <button
                type="button"
                onClick={() => { setTab('apikey'); setError('') }}
                className={`flex-1 py-2 text-sm font-medium border-b-2 transition-colors ${
                  tab === 'apikey'
                    ? 'border-blue-600 text-blue-600 dark:text-blue-400'
                    : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400'
                }`}
              >
                <KeyRound className="w-4 h-4 inline mr-1.5" />
                API Key
              </button>
            </div>
          )}

          {/* Password form */}
          {tab === 'password' && localAuthEnabled && (
            <form onSubmit={handlePasswordSubmit}>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1.5">
                Username
              </label>
              <div className="relative mb-3">
                <div className="absolute inset-y-0 left-0 flex items-center pl-3">
                  <User className="w-4 h-4 text-gray-400" />
                </div>
                <input
                  type="text"
                  value={username}
                  onChange={e => setUsername(e.target.value)}
                  placeholder="admin"
                  className="w-full pl-10 pr-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  autoFocus
                  autoComplete="username"
                />
              </div>

              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1.5">
                Password
              </label>
              <div className="relative">
                <input
                  type={showSecret ? 'text' : 'password'}
                  value={password}
                  onChange={e => setPassword(e.target.value)}
                  className="w-full pl-3 pr-10 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  autoComplete="current-password"
                />
                <button
                  type="button"
                  onClick={() => setShowSecret(!showSecret)}
                  className="absolute inset-y-0 right-0 flex items-center pr-3 text-gray-400 hover:text-gray-600"
                >
                  {showSecret ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </button>
              </div>

              {error && (
                <div className="mt-3 text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 px-3 py-2 rounded">
                  {error}
                </div>
              )}

              <button
                type="submit"
                disabled={loading || !username.trim() || !password}
                className="w-full mt-4 px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {loading ? 'Signing in...' : 'Sign In'}
              </button>
            </form>
          )}

          {/* API Key form */}
          {(tab === 'apikey' || !localAuthEnabled) && (
            <form onSubmit={handleApiKeySubmit}>
              {!localAuthEnabled && (
                <h2 className="text-lg font-semibold text-gray-900 dark:text-white mb-4">Sign In</h2>
              )}
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1.5">
                API Key
              </label>
              <div className="relative">
                <div className="absolute inset-y-0 left-0 flex items-center pl-3">
                  <KeyRound className="w-4 h-4 text-gray-400" />
                </div>
                <input
                  type={showSecret ? 'text' : 'password'}
                  value={apiKey}
                  onChange={e => setApiKey(e.target.value)}
                  placeholder="cpam_..."
                  className="w-full pl-10 pr-10 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  autoFocus={tab === 'apikey' || !localAuthEnabled}
                />
                <button
                  type="button"
                  onClick={() => setShowSecret(!showSecret)}
                  className="absolute inset-y-0 right-0 flex items-center pr-3 text-gray-400 hover:text-gray-600"
                >
                  {showSecret ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </button>
              </div>

              {error && (
                <div className="mt-3 text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 px-3 py-2 rounded">
                  {error}
                </div>
              )}

              <button
                type="submit"
                disabled={loading || !apiKey.trim()}
                className="w-full mt-4 px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {loading ? 'Signing in...' : 'Sign In'}
              </button>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}
