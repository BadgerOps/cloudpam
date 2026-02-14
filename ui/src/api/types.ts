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
  local_auth_enabled?: boolean
}

// --- User types ---

export interface UserInfo {
  id: string
  username: string
  email?: string
  display_name?: string
  role: string
  is_active: boolean
  created_at: string
  updated_at?: string
  last_login_at?: string | null
}

export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  user: UserInfo
  expires_at: string
}

export interface MeResponse {
  auth_type: 'session' | 'api_key'
  role: string
  user?: UserInfo
  key_id?: string
  key_name?: string
}

export interface CreateUserRequest {
  username: string
  email?: string
  display_name?: string
  role: string
  password: string
}

export interface UpdateUserRequest {
  email?: string
  display_name?: string
  role?: string
  is_active?: boolean
}

export interface ChangePasswordRequest {
  current_password?: string
  new_password: string
}

export interface UsersListResponse {
  users: UserInfo[]
}

// --- Discovery types ---

export type CloudResourceType = 'vpc' | 'subnet' | 'network_interface' | 'elastic_ip'
export type DiscoveryStatus = 'active' | 'stale' | 'deleted'
export type SyncJobStatus = 'pending' | 'running' | 'completed' | 'failed'

export interface DiscoveredResource {
  id: string
  account_id: number
  provider: string
  region: string
  resource_type: CloudResourceType
  resource_id: string
  name: string
  cidr?: string
  parent_resource_id?: string | null
  pool_id?: number | null
  status: DiscoveryStatus
  metadata?: Record<string, string>
  discovered_at: string
  last_seen_at: string
}

export interface DiscoveryResourcesResponse {
  items: DiscoveredResource[]
  total: number
  page: number
  page_size: number
}

export interface SyncJob {
  id: string
  account_id: number
  status: SyncJobStatus
  started_at?: string | null
  completed_at?: string | null
  resources_found: number
  resources_created: number
  resources_updated: number
  resources_deleted: number
  error_message?: string
  created_at: string
}

export interface SyncJobsResponse {
  items: SyncJob[]
}

export type AgentStatus = 'healthy' | 'stale' | 'offline'

export interface DiscoveryAgent {
  id: string
  name: string
  account_id: number
  api_key_id: string
  status: AgentStatus
  version: string
  hostname: string
  last_seen_at: string
  created_at: string
}

export interface DiscoveryAgentsResponse {
  items: DiscoveryAgent[]
}

// --- Recommendation types ---

export type RecommendationType = 'allocation' | 'compliance'
export type RecommendationStatus = 'pending' | 'applied' | 'dismissed'
export type RecommendationPriority = 'high' | 'medium' | 'low'

export interface Recommendation {
  id: string
  pool_id: number
  type: RecommendationType
  status: RecommendationStatus
  priority: RecommendationPriority
  title: string
  description: string
  suggested_cidr?: string
  rule_id?: string
  score: number
  metadata?: Record<string, string>
  dismiss_reason?: string
  applied_pool_id?: number | null
  created_at: string
  updated_at: string
}

export interface RecommendationsListResponse {
  items: Recommendation[]
  total: number
  page: number
  page_size: number
}

export interface GenerateRecommendationsResponse {
  items: Recommendation[]
  total: number
}
