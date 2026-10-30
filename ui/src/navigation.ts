import {
  Bot,
  Clock,
  Cloud,
  FileText,
  GitCompareArrows,
  Key,
  LayoutDashboard,
  LayoutGrid,
  Lightbulb,
  Map as MapIcon,
  Radio,
  RefreshCw,
  Server,
  Shield,
  UserCog,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

export const NAV_SECTIONS = ['IPAM', 'Operations', 'Planning', 'Administration'] as const

export type NavSection = (typeof NAV_SECTIONS)[number]

export interface NavEntry {
  label: string
  icon: LucideIcon
  section?: NavSection
}

interface RouteSpec {
  path: string
  // Any one of these opens the route. Each list mirrors the permission the
  // page's primary API endpoint enforces in internal/api.
  requiredAnyPermissions?: readonly string[]
  nav?: NavEntry
}

const ROUTE_SPECS = [
  { path: '/', nav: { label: 'Dashboard', icon: LayoutDashboard } },
  { path: '/pools', nav: { label: 'Address Pools', icon: Server, section: 'IPAM' } },
  { path: '/blocks', nav: { label: 'Allocated Blocks', icon: LayoutGrid, section: 'IPAM' } },
  { path: '/accounts', nav: { label: 'Cloud Accounts', icon: Cloud, section: 'IPAM' } },
  { path: '/discovery', nav: { label: 'Discovery', icon: RefreshCw, section: 'Operations' } },
  { path: '/drift', nav: { label: 'Drift Detection', icon: GitCompareArrows, section: 'Operations' } },
  { path: '/audit', requiredAnyPermissions: ['audit:read'], nav: { label: 'Audit Log', icon: Clock, section: 'Operations' } },
  { path: '/schema', nav: { label: 'Schema Planner', icon: MapIcon, section: 'Planning' } },
  { path: '/recommendations', nav: { label: 'Recommendations', icon: Lightbulb, section: 'Planning' } },
  { path: '/ai-planner', nav: { label: 'AI Planner', icon: Bot, section: 'Planning' } },
  { path: '/config/api-keys', requiredAnyPermissions: ['apikeys:list', 'apikeys:read'], nav: { label: 'API Keys', icon: Key, section: 'Administration' } },
  { path: '/changelog', nav: { label: 'Release Notes', icon: FileText, section: 'Administration' } },
  { path: '/identity', requiredAnyPermissions: ['settings:read', 'users:list'], nav: { label: 'Identity', icon: UserCog, section: 'Administration' } },
  { path: '/config', requiredAnyPermissions: ['settings:read'], nav: { label: 'Configuration', icon: Shield, section: 'Administration' } },
  { path: '/config/log-destinations', requiredAnyPermissions: ['settings:read'], nav: { label: 'Log Destinations', icon: Radio, section: 'Administration' } },
  { path: '/config/updates', requiredAnyPermissions: ['settings:read'] },
  { path: '/config/security', requiredAnyPermissions: ['settings:read'] },
  { path: '/config/users', requiredAnyPermissions: ['users:list'] },
  { path: '/profile' },
] as const satisfies readonly RouteSpec[]

export type RoutePath = (typeof ROUTE_SPECS)[number]['path']

export type AppRoute = RouteSpec & { path: RoutePath }

export const APP_ROUTES: readonly AppRoute[] = ROUTE_SPECS

export function canOpen(path: RoutePath, hasPermission: (permission: string) => boolean): boolean {
  const required = APP_ROUTES.find(route => route.path === path)?.requiredAnyPermissions
  return !required?.length || required.some(hasPermission)
}
