import { NavLink, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard,
  Server,
  LayoutGrid,
  Cloud,
  RefreshCw,
  Clock,
  Upload,
  Key,
  Map,
  Sun,
  Moon,
  Monitor,
  LogOut,
  Shield,
} from 'lucide-react'
import { useTheme } from '../hooks/useTheme'
import { useAuth } from '../hooks/useAuth'

const navItems = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard', end: true },
  { to: '/pools', icon: Server, label: 'Address Pools' },
  { to: '/blocks', icon: LayoutGrid, label: 'Allocated Blocks' },
  { to: '/accounts', icon: Cloud, label: 'Cloud Accounts' },
  { to: '/discovery', icon: RefreshCw, label: 'Discovery' },
  { to: '/audit', icon: Clock, label: 'Audit Log' },
  { to: '/schema', icon: Map, label: 'Schema Planner' },
]

interface SidebarProps {
  onImportExport: () => void
}

export default function Sidebar({ onImportExport }: SidebarProps) {
  const { mode, cycle } = useTheme()
  const { isAuthenticated, authEnabled, keyName, role, logout } = useAuth()
  const navigate = useNavigate()
  const ThemeIcon = mode === 'dark' ? Moon : mode === 'light' ? Sun : Monitor
  const themeLabel = mode === 'system' ? 'System' : mode === 'dark' ? 'Dark' : 'Light'

  function handleLogout() {
    logout()
    navigate('/login')
  }

  return (
    <aside className="w-64 bg-gray-900 dark:bg-gray-950 text-white flex flex-col flex-shrink-0">
      <div className="p-4 border-b border-gray-800">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-blue-600 rounded-lg">
            <Server className="w-6 h-6" />
          </div>
          <div>
            <h1 className="font-bold text-lg">CloudPAM</h1>
            <p className="text-xs text-gray-400">IP Address Management</p>
          </div>
        </div>
      </div>

      <nav aria-label="Main navigation" className="flex-1 p-4 space-y-1">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            className={({ isActive }) =>
              `w-full flex items-center gap-3 px-3 py-2 rounded-lg transition-colors ${
                isActive ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-800'
              }`
            }
          >
            <item.icon className="w-5 h-5" />
            <span>{item.label}</span>
          </NavLink>
        ))}
      </nav>

      <div className="p-4 border-t border-gray-800 space-y-1">
        {/* Auth info */}
        {authEnabled && isAuthenticated && (
          <div className="px-3 py-2 mb-2">
            <div className="flex items-center gap-2">
              <Shield className="w-4 h-4 text-gray-400" />
              <span className="text-xs text-gray-400 truncate" title={keyName ?? undefined}>
                {keyName ?? 'API Key'}
              </span>
              {role && (
                <span className="ml-auto px-1.5 py-0.5 bg-blue-600/30 text-blue-300 rounded text-[10px] font-medium uppercase">
                  {role}
                </span>
              )}
            </div>
          </div>
        )}

        <button
          onClick={onImportExport}
          className="w-full flex items-center gap-3 px-3 py-2 text-gray-300 hover:bg-gray-800 rounded-lg"
        >
          <Upload className="w-5 h-5" />
          <span>Import/Export</span>
        </button>

        {/* API Keys link - admin only (or when no auth) */}
        {(!authEnabled || role === 'admin') && (
          <NavLink
            to="/settings/api-keys"
            className={({ isActive }) =>
              `w-full flex items-center gap-3 px-3 py-2 rounded-lg transition-colors ${
                isActive ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-800'
              }`
            }
          >
            <Key className="w-5 h-5" />
            <span>API Keys</span>
          </NavLink>
        )}

        <button
          onClick={cycle}
          aria-label={`Theme: ${themeLabel}. Click to cycle.`}
          className="w-full flex items-center gap-3 px-3 py-2 text-gray-300 hover:bg-gray-800 rounded-lg"
        >
          <ThemeIcon className="w-5 h-5" />
          <span>{themeLabel}</span>
        </button>

        {/* Logout button */}
        {authEnabled && isAuthenticated && (
          <button
            onClick={handleLogout}
            className="w-full flex items-center gap-3 px-3 py-2 text-gray-300 hover:bg-gray-800 rounded-lg"
          >
            <LogOut className="w-5 h-5" />
            <span>Logout</span>
          </button>
        )}
      </div>
    </aside>
  )
}
