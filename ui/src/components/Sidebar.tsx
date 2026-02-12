import { NavLink } from 'react-router-dom'
import {
  LayoutDashboard,
  Server,
  LayoutGrid,
  Cloud,
  RefreshCw,
  Clock,
  Upload,
  Settings,
  Map,
  Sun,
  Moon,
  Monitor,
} from 'lucide-react'
import { useTheme } from '../hooks/useTheme'

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
  const ThemeIcon = mode === 'dark' ? Moon : mode === 'light' ? Sun : Monitor
  const themeLabel = mode === 'system' ? 'System' : mode === 'dark' ? 'Dark' : 'Light'

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

      <div className="p-4 border-t border-gray-800">
        <button
          onClick={onImportExport}
          className="w-full flex items-center gap-3 px-3 py-2 text-gray-300 hover:bg-gray-800 rounded-lg"
        >
          <Upload className="w-5 h-5" />
          <span>Import/Export</span>
        </button>
        <button className="w-full flex items-center gap-3 px-3 py-2 text-gray-300 hover:bg-gray-800 rounded-lg mt-1">
          <Settings className="w-5 h-5" />
          <span>Settings</span>
        </button>
        <button
          onClick={cycle}
          aria-label={`Theme: ${themeLabel}. Click to cycle.`}
          className="w-full flex items-center gap-3 px-3 py-2 text-gray-300 hover:bg-gray-800 rounded-lg mt-1"
        >
          <ThemeIcon className="w-5 h-5" />
          <span>{themeLabel}</span>
        </button>
      </div>
    </aside>
  )
}
