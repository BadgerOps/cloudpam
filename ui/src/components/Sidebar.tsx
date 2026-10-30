import { NavLink, useNavigate } from 'react-router-dom'
import { Server, Upload, Settings, X } from 'lucide-react'
import { useAuth } from '../hooks/useAuth'
import { APP_ROUTES, NAV_SECTIONS, canOpen } from '../navigation'
import type { AppRoute, NavEntry, NavSection } from '../navigation'

const linkClass = ({ isActive }: { isActive: boolean }) =>
  `w-full flex items-center gap-3 px-3 py-2 rounded-lg transition-colors ${
    isActive ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-800'
  }`

const sectionHeader = 'px-3 py-1 text-[10px] font-semibold uppercase tracking-wider text-gray-500'

type NavRoute = AppRoute & { nav: NavEntry }

function NavItem({ route }: { route: NavRoute }) {
  const Icon = route.nav.icon
  return (
    <NavLink to={route.path} end={route.path === '/'} className={linkClass}>
      <Icon className="w-5 h-5 flex-shrink-0" />
      <span>{route.nav.label}</span>
    </NavLink>
  )
}

interface SidebarProps {
  onImportExport: () => void
  open: boolean
  onClose: () => void
}

export default function Sidebar({ onImportExport, open, onClose }: SidebarProps) {
  const { isAuthenticated, hasPermission } = useAuth()
  const navigate = useNavigate()
  const links = APP_ROUTES.filter(
    (route): route is NavRoute => route.nav !== undefined && canOpen(route.path, hasPermission),
  )
  const inSection = (section?: NavSection) => links.filter(route => route.nav.section === section)

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
        {inSection(undefined).map(route => <NavItem key={route.path} route={route} />)}

        {NAV_SECTIONS.map(section => {
          const items = inSection(section)
          if (items.length === 0) return null
          return (
            <div key={section} className="pt-3 mt-3 border-t border-gray-800">
              <p className={sectionHeader}>{section}</p>
              {items.map(route => <NavItem key={route.path} route={route} />)}
            </div>
          )
        })}
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
