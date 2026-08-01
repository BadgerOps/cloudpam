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
  Settings,
  Lightbulb,
  Bot,
  Radio,
  Shield,
  GitCompareArrows,
  FileText,
  UserCog,
  X,
} from 'lucide-react'
import { useAuth } from '../hooks/useAuth'

const linkClass = ({ isActive }: { isActive: boolean }) =>
  `w-full flex items-center gap-3 px-3 py-2 rounded-lg transition-colors ${
    isActive ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-800'
  }`

const sectionHeader = 'px-3 py-1 text-[10px] font-semibold uppercase tracking-wider text-gray-500'

interface SidebarProps {
  onImportExport: () => void
  open: boolean
  onClose: () => void
}

export default function Sidebar({ onImportExport, open, onClose }: SidebarProps) {
  const { isAuthenticated, hasPermission } = useAuth()
  const navigate = useNavigate()
  const canSettings = hasPermission('settings:read')
  const canUsers = hasPermission('users:list')
  const canAudit = hasPermission('audit:read') || hasPermission('audit:list')

  return (
    <aside
      id="main-sidebar"
      data-testid="sidebar"
      className={`fixed inset-y-0 left-0 z-40 w-64 bg-gray-900 dark:bg-gray-950 text-white flex flex-col flex-shrink-0 transition-transform duration-200 ease-in-out md:static md:translate-x-0 ${
        open ? 'translate-x-0' : '-translate-x-full'
      }`}
    >
      <div className="p-4 border-b border-gray-800">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-blue-600 rounded-lg flex-shrink-0">
            <Server className="w-6 h-6" />
          </div>
          <div>
            <h1 className="font-bold text-lg">CloudPAM</h1>
            <p className="text-xs text-gray-400">IP Address Management</p>
          </div>
          <button
            onClick={onClose}
            aria-label="Close navigation menu"
            className="ml-auto p-1 text-gray-400 hover:text-white hover:bg-gray-800 rounded md:hidden"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
      </div>

      <nav aria-label="Main navigation" className="flex-1 p-4 space-y-1 overflow-y-auto">
        <NavLink to="/" end className={linkClass}>
          <LayoutDashboard className="w-5 h-5 flex-shrink-0" />
          <span>Dashboard</span>
        </NavLink>

        {/* IPAM section */}
        <div className="pt-3 mt-3 border-t border-gray-800">
          <p className={sectionHeader}>IPAM</p>
          <NavLink to="/pools" className={linkClass}>
            <Server className="w-5 h-5 flex-shrink-0" />
            <span>Address Pools</span>
          </NavLink>
          <NavLink to="/blocks" className={linkClass}>
            <LayoutGrid className="w-5 h-5 flex-shrink-0" />
            <span>Allocated Blocks</span>
          </NavLink>
          <NavLink to="/accounts" className={linkClass}>
            <Cloud className="w-5 h-5 flex-shrink-0" />
            <span>Cloud Accounts</span>
          </NavLink>
        </div>

        {/* Operations section */}
        <div className="pt-3 mt-3 border-t border-gray-800">
          <p className={sectionHeader}>Operations</p>
          <NavLink to="/discovery" className={linkClass}>
            <RefreshCw className="w-5 h-5 flex-shrink-0" />
            <span>Discovery</span>
          </NavLink>
          <NavLink to="/drift" className={linkClass}>
            <GitCompareArrows className="w-5 h-5 flex-shrink-0" />
            <span>Drift Detection</span>
          </NavLink>
          {canAudit && (
            <NavLink to="/audit" className={linkClass}>
              <Clock className="w-5 h-5 flex-shrink-0" />
              <span>Audit Log</span>
            </NavLink>
          )}
        </div>

        {/* Planning section */}
        <div className="pt-3 mt-3 border-t border-gray-800">
          <p className={sectionHeader}>Planning</p>
          <NavLink to="/schema" className={linkClass}>
            <Map className="w-5 h-5 flex-shrink-0" />
            <span>Schema Planner</span>
          </NavLink>
          <NavLink to="/recommendations" className={linkClass}>
            <Lightbulb className="w-5 h-5 flex-shrink-0" />
            <span>Recommendations</span>
          </NavLink>
          <NavLink to="/ai-planner" className={linkClass}>
            <Bot className="w-5 h-5 flex-shrink-0" />
            <span>AI Planner</span>
          </NavLink>
        </div>

        {/* Configuration section */}
        <div className="pt-3 mt-3 border-t border-gray-800">
          <p className={sectionHeader}>Administration</p>
          <NavLink to="/config/api-keys" className={linkClass}>
            <Key className="w-5 h-5 flex-shrink-0" />
            <span>API Keys</span>
          </NavLink>
          <NavLink to="/changelog" className={linkClass}>
            <FileText className="w-5 h-5 flex-shrink-0" />
            <span>Release Notes</span>
          </NavLink>
          {(canSettings || canUsers) && (
            <NavLink to="/identity" className={linkClass}>
              <UserCog className="w-5 h-5 flex-shrink-0" />
              <span>Identity</span>
            </NavLink>
          )}
          {canSettings && (
            <NavLink to="/config" className={linkClass}>
              <Shield className="w-5 h-5 flex-shrink-0" />
              <span>Configuration</span>
            </NavLink>
          )}
          {canSettings && (
            <NavLink to="/config/log-destinations" className={linkClass}>
              <Radio className="w-5 h-5 flex-shrink-0" />
              <span>Log Destinations</span>
            </NavLink>
          )}
        </div>
      </nav>

      <div className="p-4 border-t border-gray-800 space-y-1">
        <button
          onClick={onImportExport}
          className="w-full flex items-center gap-3 px-3 py-2 text-gray-300 hover:bg-gray-800 rounded-lg"
          aria-label="Import/Export"
        >
          <Upload className="w-5 h-5 flex-shrink-0" />
          <span>Import/Export</span>
        </button>

        {!isAuthenticated && (
          <button
            onClick={() => navigate('/login')}
            className="w-full flex items-center gap-3 px-3 py-2 text-gray-300 hover:bg-gray-800 rounded-lg"
            aria-label="Login"
          >
            <Settings className="w-5 h-5 flex-shrink-0" />
            <span>Login</span>
          </button>
        )}
      </div>
    </aside>
  )
}
