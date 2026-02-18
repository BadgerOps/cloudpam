import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Eye, EyeOff, Server, ShieldCheck } from 'lucide-react'
import { useAuth } from '../hooks/useAuth'

export default function SetupPage() {
  const { needsSetup, authChecked } = useAuth()
  const navigate = useNavigate()

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [email, setEmail] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  // Redirect away if setup is already complete
  useEffect(() => {
    if (authChecked && !needsSetup) {
      navigate('/login', { replace: true })
    }
  }, [authChecked, needsSetup, navigate])

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!username.trim() || !password) return

    if (password !== confirmPassword) {
      setError('Passwords do not match')
      return
    }

    if (password.length < 12) {
      setError('Password must be at least 12 characters')
      return
    }

    setLoading(true)
    setError('')

    try {
      const res = await fetch('/api/v1/auth/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username: username.trim(),
          password,
          email: email.trim() || undefined,
        }),
      })

      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: 'Setup failed' }))
        throw new Error(body.error || 'Setup failed')
      }

      // Setup complete — redirect to login
      navigate('/login', { replace: true })
      // Force a page reload so healthz is re-fetched with needs_setup=false
      window.location.href = '/login'
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Setup failed')
    } finally {
      setLoading(false)
    }
  }

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
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">CloudPAM Setup</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Create your admin account to get started</p>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-xl shadow-lg p-6">
          <div className="flex items-center gap-2 mb-4 text-sm text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/20 px-3 py-2 rounded">
            <ShieldCheck className="w-4 h-4 flex-shrink-0" />
            <span>This is a fresh install. Create an admin account to secure your instance.</span>
          </div>

          <form onSubmit={handleSubmit}>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1.5">
              Username
            </label>
            <input
              type="text"
              value={username}
              onChange={e => setUsername(e.target.value)}
              placeholder="admin"
              className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-blue-500 mb-3"
              autoFocus
              autoComplete="username"
            />

            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1.5">
              Email <span className="text-gray-400 font-normal">(optional)</span>
            </label>
            <input
              type="email"
              value={email}
              onChange={e => setEmail(e.target.value)}
              placeholder="admin@example.com"
              className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-blue-500 mb-3"
              autoComplete="email"
            />

            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1.5">
              Password
            </label>
            <div className="relative mb-3">
              <input
                type={showPassword ? 'text' : 'password'}
                value={password}
                onChange={e => setPassword(e.target.value)}
                placeholder="Minimum 12 characters"
                className="w-full pl-3 pr-10 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                autoComplete="new-password"
              />
              <button
                type="button"
                onClick={() => setShowPassword(!showPassword)}
                className="absolute inset-y-0 right-0 flex items-center pr-3 text-gray-400 hover:text-gray-600"
              >
                {showPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
              </button>
            </div>

            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1.5">
              Confirm Password
            </label>
            <input
              type={showPassword ? 'text' : 'password'}
              value={confirmPassword}
              onChange={e => setConfirmPassword(e.target.value)}
              className="w-full px-3 py-2 border rounded-lg text-sm dark:bg-gray-700 dark:border-gray-600 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
              autoComplete="new-password"
            />

            {error && (
              <div className="mt-3 text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 px-3 py-2 rounded">
                {error}
              </div>
            )}

            <button
              type="submit"
              disabled={loading || !username.trim() || !password || !confirmPassword}
              className="w-full mt-4 px-4 py-2 bg-blue-600 text-white rounded-lg text-sm font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {loading ? 'Creating account...' : 'Create Admin Account'}
            </button>
          </form>
        </div>
      </div>
    </div>
  )
}
