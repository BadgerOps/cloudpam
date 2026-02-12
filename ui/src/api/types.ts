export interface SchemaPoolEntry {
  ref: string
  name: string
  cidr: string
  type: string
  parent_ref: string | null
  description?: string
}

export interface SchemaCheckRequest {
  pools: SchemaPoolEntry[]
}

export interface SchemaConflict {
  planned_cidr: string
  planned_name: string
  existing_pool_id: number
  existing_pool_name: string
  existing_cidr: string
  overlap_type: string
}

export interface SchemaCheckResponse {
  conflicts: SchemaConflict[]
  total_pools: number
  conflict_count: number
}

export interface SchemaApplyRequest {
  pools: SchemaPoolEntry[]
  status: string
  tags: Record<string, string>
  skip_conflicts: boolean
}

export interface SchemaApplyResponse {
  created: number
  skipped: number
  errors: string[]
  root_pool_id: number
  pool_map: Record<string, number>
}

export interface ApiError {
  error: string
  detail?: string
}

// --- Domain types ---

export type PoolType = 'supernet' | 'region' | 'environment' | 'vpc' | 'subnet'
export type PoolStatus = 'planned' | 'active' | 'deprecated'
export type PoolSource = 'manual' | 'discovered' | 'imported'

export interface Pool {
  id: number
  name: string
  cidr: string
  parent_id?: number | null
  account_id?: number | null
  type: PoolType
  status: PoolStatus
  source: PoolSource
  description?: string
  tags?: Record<string, string>
  created_at: string
  updated_at: string
}

export interface PoolStats {
  total_ips: number
  used_ips: number
  available_ips: number
  utilization: number
  child_count: number
  direct_children: number
}

export interface PoolWithStats extends Pool {
  stats: PoolStats
  children?: PoolWithStats[]
}

export interface CreatePoolRequest {
  name: string
  cidr: string
  parent_id?: number | null
  account_id?: number | null
  type?: PoolType
  status?: PoolStatus
  source?: PoolSource
  description?: string
  tags?: Record<string, string>
}

export interface Account {
  id: number
  key: string
  name: string
  provider?: string
  external_id?: string
  description?: string
  platform?: string
  tier?: string
  environment?: string
  regions?: string[]
  created_at: string
}

export interface CreateAccountRequest {
  key: string
  name: string
  provider?: string
  external_id?: string
  description?: string
  platform?: string
  tier?: string
  environment?: string
  regions?: string[]
}

export interface Block {
  id: number
  name: string
  cidr: string
  parent_id: number
  parent_name: string
  account_id?: number
  account_name?: string
  account_platform?: string
  account_tier?: string
  account_environment?: string
  account_regions?: string[]
  type?: string
  status?: string
  created_at: string
}

export interface BlocksListResponse {
  items: Block[]
  total: number
  page: number
  page_size: number
}

export interface AuditChanges {
  before?: Record<string, unknown>
  after?: Record<string, unknown>
}

export interface AuditEvent {
  id: string
  timestamp: string
  actor: string
  actor_type: string
  action: string
  resource_type: string
  resource_id: string
  resource_name?: string
  changes?: AuditChanges
  request_id?: string
  ip_address?: string
  status_code: number
}

export interface AuditListResponse {
  events: AuditEvent[]
  total: number
  limit: number
  offset: number
}

export interface ImportResult {
  created: number
  skipped: number
  errors: string[]
}

// --- Search types ---

export interface SearchResultItem {
  type: 'pool' | 'account'
  id: number
  name: string
  cidr?: string
  description?: string
  status?: string
  pool_type?: string
  account_key?: string
  provider?: string
  parent_id?: number | null
  account_id?: number | null
}

export interface SearchResponse {
  items: SearchResultItem[]
  total: number
  page: number
  page_size: number
}

// --- Auth types ---

export interface ApiKeyInfo {
  id: string
  prefix: string
  name: string
  scopes: string[]
  created_at: string
  expires_at?: string | null
  last_used_at?: string | null
  revoked: boolean
}

export interface ApiKeyCreateRequest {
  name: string
  scopes?: string[]
  expires_in_days?: number
}

export interface ApiKeyCreateResponse {
  id: string
  key: string
  prefix: string
  name: string
  scopes: string[]
  created_at: string
  expires_at?: string | null
}

export interface HealthResponse {
  status: string
  auth_enabled?: boolean
}
