# Sprint 5: Enhanced Pool Model & UI

## Overview

This sprint implements the enhanced Pool model with types, status, and utilization tracking, along with a modernized UI matching the mockups in `.planning/mockups/`.

**Branch**: `sprint5/enhanced-pool-model-ui`
**Target**: Complete hierarchical tree view, pool types, utilization, and dashboard

---

## Phase 5A: Domain Model Enhancement

### Task 5A.1: Enhanced Pool Model
**File**: `internal/domain/types.go`

Add new fields to the Pool struct:
```go
type PoolType string
const (
    PoolTypeSupernet    PoolType = "supernet"
    PoolTypeRegion      PoolType = "region"
    PoolTypeEnvironment PoolType = "environment"
    PoolTypeVPC         PoolType = "vpc"
    PoolTypeSubnet      PoolType = "subnet"
)

type PoolStatus string
const (
    PoolStatusPlanned    PoolStatus = "planned"
    PoolStatusActive     PoolStatus = "active"
    PoolStatusDeprecated PoolStatus = "deprecated"
)

type PoolSource string
const (
    PoolSourceManual     PoolSource = "manual"
    PoolSourceDiscovered PoolSource = "discovered"
    PoolSourceImported   PoolSource = "imported"
)

type Pool struct {
    ID          int64             `json:"id"`
    Name        string            `json:"name"`
    CIDR        string            `json:"cidr"`
    ParentID    *int64            `json:"parent_id,omitempty"`
    AccountID   *int64            `json:"account_id,omitempty"`
    Type        PoolType          `json:"type"`
    Status      PoolStatus        `json:"status"`
    Source      PoolSource        `json:"source"`
    Description string            `json:"description,omitempty"`
    Tags        map[string]string `json:"tags,omitempty"`
    CreatedAt   time.Time         `json:"created_at"`
    UpdatedAt   time.Time         `json:"updated_at"`
}
```

### Task 5A.2: Pool Statistics
**File**: `internal/domain/types.go`

Add computed statistics:
```go
type PoolStats struct {
    TotalIPs      int64   `json:"total_ips"`
    UsedIPs       int64   `json:"used_ips"`
    AvailableIPs  int64   `json:"available_ips"`
    Utilization   float64 `json:"utilization"` // 0-100 percentage
    ChildCount    int     `json:"child_count"`
    DirectChildren int    `json:"direct_children"`
}

type PoolWithStats struct {
    Pool
    Stats    PoolStats  `json:"stats"`
    Children []PoolWithStats `json:"children,omitempty"`
}
```

### Task 5A.3: Update CreatePool
**File**: `internal/domain/types.go`

```go
type CreatePool struct {
    Name        string            `json:"name"`
    CIDR        string            `json:"cidr"`
    ParentID    *int64            `json:"parent_id,omitempty"`
    AccountID   *int64            `json:"account_id,omitempty"`
    Type        PoolType          `json:"type,omitempty"`
    Status      PoolStatus        `json:"status,omitempty"`
    Source      PoolSource        `json:"source,omitempty"`
    Description string            `json:"description,omitempty"`
    Tags        map[string]string `json:"tags,omitempty"`
}
```

---

## Phase 5B: Storage Layer Updates

### Task 5B.1: Update Store Interface
**File**: `internal/storage/store.go`

Add new methods:
```go
type Store interface {
    // Existing methods...

    // New methods for enhanced pools
    GetPoolWithStats(ctx context.Context, id int64) (*domain.PoolWithStats, error)
    GetPoolHierarchy(ctx context.Context, rootID *int64) ([]domain.PoolWithStats, error)
    GetPoolChildren(ctx context.Context, parentID int64) ([]domain.Pool, error)
    UpdatePoolStatus(ctx context.Context, id int64, status domain.PoolStatus) error
    CalculatePoolUtilization(ctx context.Context, id int64) (*domain.PoolStats, error)
}
```

### Task 5B.2: Update MemoryStore
**File**: `internal/storage/store.go`

Implement new methods in MemoryStore with proper utilization calculation.

### Task 5B.3: Update SQLite Store
**File**: `internal/storage/sqlite/sqlite.go`

- Add migration for new columns (type, status, source, description, tags, updated_at)
- Implement new interface methods
- Use recursive CTE for hierarchy queries

**Migration**: `migrations/0003_enhanced_pools.sql`
```sql
ALTER TABLE pools ADD COLUMN type TEXT NOT NULL DEFAULT 'subnet';
ALTER TABLE pools ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE pools ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE pools ADD COLUMN description TEXT;
ALTER TABLE pools ADD COLUMN tags TEXT; -- JSON
ALTER TABLE pools ADD COLUMN updated_at TEXT;
```

---

## Phase 5C: API Enhancements

### Task 5C.1: Hierarchy Endpoint
**File**: `internal/api/server.go`

New endpoint: `GET /api/v1/pools/hierarchy`
```json
{
  "pools": [
    {
      "id": 1,
      "name": "Root",
      "cidr": "10.0.0.0/8",
      "type": "supernet",
      "status": "active",
      "stats": {
        "total_ips": 16777216,
        "used_ips": 2097152,
        "utilization": 12.5,
        "child_count": 3
      },
      "children": [
        {
          "id": 2,
          "name": "us-east-1",
          "cidr": "10.0.0.0/12",
          "type": "region",
          "children": [...]
        }
      ]
    }
  ]
}
```

### Task 5C.2: Pool Stats Endpoint
**File**: `internal/api/server.go`

New endpoint: `GET /api/v1/pools/{id}/stats`
```json
{
  "total_ips": 65536,
  "used_ips": 16384,
  "available_ips": 49152,
  "utilization": 25.0,
  "child_count": 4,
  "direct_children": 2
}
```

### Task 5C.3: Update Existing Endpoints
**File**: `internal/api/server.go`

- `POST /api/v1/pools` - Accept type, status, source, description, tags
- `PATCH /api/v1/pools/{id}` - Allow updating type, status, description, tags
- `GET /api/v1/pools` - Include stats in response (optional via query param)

---

## Phase 5D: Frontend Overhaul

### Task 5D.1: Sidebar Navigation
**File**: `web/index.html`

Replace header nav with sidebar matching mockup:
- Dashboard
- Address Pools
- Cloud Accounts
- Discovery (placeholder)
- Audit Log

### Task 5D.2: Dashboard View
**File**: `web/index.html`

New dashboard with:
- Stats cards (Total Pools, Allocated IPs, Cloud Accounts, Active Alerts)
- Address Space tree view
- Alerts panel
- Cloud Accounts table
- Recent Activity feed

### Task 5D.3: Hierarchical Tree View
**File**: `web/index.html`

Replace flat table with expandable tree:
- Collapsible nodes with chevron icons
- Type indicators (colored dots)
- Utilization bars
- Status badges (synced, drift, planned)
- Source badges (manual, cloud)
- Host count display

### Task 5D.4: Pool Detail Panel
**File**: `web/index.html`

Slide-out panel when pool selected:
- Pool metadata (type, source, status)
- Utilization visualization
- Cloud resource info (if discovered)
- Drift alerts (if applicable)
- Actions: Allocate Subnet, View Audit Log

### Task 5D.5: Style Updates
**File**: `web/index.html`

- Dark sidebar theme
- Updated color scheme matching mockups
- Improved spacing and typography
- Responsive layout

---

## Phase 5E: Testing & Documentation

### Task 5E.1: Unit Tests
- Test new Pool model fields
- Test storage layer updates
- Test utilization calculations
- Test hierarchy queries

### Task 5E.2: Integration Tests
- Test hierarchy endpoint
- Test stats calculation
- Test CRUD with new fields

### Task 5E.3: Update API Documentation
**File**: `docs/openapi.yaml`

Add new schemas and endpoints.

### Task 5E.4: Update CHANGELOG
**File**: `docs/CHANGELOG.md`

Document all Sprint 5 changes.

---

## Implementation Order

1. **5A** (Domain Model) - Foundation for everything else
2. **5B** (Storage) - Persist new fields
3. **5C** (API) - Expose via REST
4. **5D** (Frontend) - User-facing changes
5. **5E** (Testing) - Ensure quality

## Parallel Execution Strategy

Can run in parallel:
- 5A + 5B (model + storage) - one agent
- 5C (API) - second agent after 5A/5B complete
- 5D (Frontend) - can start after 5C provides endpoints
- 5E (Tests) - incremental as features complete

## Estimated Scope

- **Domain Model**: ~100 lines
- **Storage Updates**: ~300 lines (memory + sqlite)
- **API Changes**: ~200 lines
- **Frontend**: ~800 lines (significant refactor)
- **Tests**: ~400 lines
- **Total**: ~1800 lines

## Success Criteria

1. Pool model supports type, status, source, description, tags
2. Utilization calculated correctly from child pools
3. Hierarchy endpoint returns nested tree structure
4. UI displays hierarchical tree with all mockup features
5. All tests pass with 80%+ coverage on new code
6. No regressions in existing functionality
