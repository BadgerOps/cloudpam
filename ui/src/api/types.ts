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
