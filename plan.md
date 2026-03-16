# Drift Detection Implementation Plan

## Overview

Compare discovered cloud resources against managed IPAM pools and surface mismatches as "drift items" — resources that exist in the cloud but not in IPAM (unmanaged), pools that exist in IPAM but not in the cloud (orphaned), and CIDR/metadata mismatches between linked pairs.

## Drift Types

| Type | Description |
|------|-------------|
| `unmanaged` | Cloud resource exists but has no linked IPAM pool |
| `orphaned` | IPAM pool is marked as discovered/cloud-linked but the resource is stale/deleted |
| `cidr_mismatch` | Linked resource and pool have different CIDRs |
| `metadata_mismatch` | Linked resource and pool disagree on name, region, or account |

## Implementation Steps

### 1. Domain Types (`internal/domain/drift.go`)

```go
type DriftType string
const (
    DriftTypeUnmanaged        DriftType = "unmanaged"
    DriftTypeOrphaned         DriftType = "orphaned"
    DriftTypeCIDRMismatch     DriftType = "cidr_mismatch"
    DriftTypeMetadataMismatch DriftType = "metadata_mismatch"
)

type DriftSeverity string
const (
    DriftSeverityCritical DriftSeverity = "critical"
    DriftSeverityWarning  DriftSeverity = "warning"
    DriftSeverityInfo     DriftSeverity = "info"
)

type DriftItem struct {
    ID             string            `json:"id"`
    AccountID      int64             `json:"account_id"`
    Type           DriftType         `json:"type"`
    Severity       DriftSeverity     `json:"severity"`
    ResourceID     *uuid.UUID        `json:"resource_id,omitempty"`     // discovered resource
    PoolID         *int64            `json:"pool_id,omitempty"`         // IPAM pool
    ResourceCIDR   string            `json:"resource_cidr,omitempty"`
    PoolCIDR       string            `json:"pool_cidr,omitempty"`
    Description    string            `json:"description"`
    Details        map[string]string `json:"details,omitempty"`
    Status         DriftStatus       `json:"status"`
    ResolvedAt     *time.Time        `json:"resolved_at,omitempty"`
    DetectedAt     time.Time         `json:"detected_at"`
}

type DriftStatus string
const (
    DriftStatusOpen     DriftStatus = "open"
    DriftStatusResolved DriftStatus = "resolved"
    DriftStatusIgnored  DriftStatus = "ignored"
)
```

Plus request/response types: `DriftDetectionRequest`, `DriftDetectionResponse`, `DriftFilters`, `DriftListResponse`.

### 2. Drift Detection Engine (`internal/planning/drift.go`)

A `DriftService` that takes the main `Store` + `DiscoveryStore` and runs comparisons:

- **`DetectDrift(ctx, accountID) → []DriftItem`**: main entry point
  1. Load all active discovered resources for the account (VPCs + subnets with CIDRs)
  2. Load all pools for the account
  3. Cross-reference:
     - Resources with no `pool_id` → `unmanaged` (severity: warning)
     - Pools with `source=discovered` whose linked resource is stale/deleted → `orphaned` (severity: critical)
     - Linked pairs where resource.CIDR ≠ pool.CIDR → `cidr_mismatch` (severity: critical)
     - Linked pairs where name/region differs → `metadata_mismatch` (severity: info)
  4. Return the drift items

### 3. Storage Interface (`internal/storage/drift.go`)

```go
type DriftStore interface {
    CreateDriftItem(ctx, item DriftItem) error
    GetDriftItem(ctx, id string) (*DriftItem, error)
    ListDriftItems(ctx, filters DriftFilters) ([]DriftItem, int, error)
    UpdateDriftStatus(ctx, id string, status DriftStatus) error
    DeleteResolvedForAccount(ctx, accountID int64) error
}
```

### 4. Storage Implementations

- **Memory**: `internal/storage/drift_memory.go` — `MemoryDriftStore` (same pattern as recommendations)
- **SQLite**: `internal/storage/sqlite/drift.go`
- **Migration**: `migrations/0018_drift_items.sql`

```sql
CREATE TABLE IF NOT EXISTS drift_items (
    id TEXT PRIMARY KEY,
    account_id INTEGER NOT NULL,
    type TEXT NOT NULL,
    severity TEXT NOT NULL,
    resource_id TEXT,
    pool_id INTEGER,
    resource_cidr TEXT,
    pool_cidr TEXT,
    description TEXT NOT NULL,
    details TEXT DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'open',
    resolved_at TEXT,
    detected_at TEXT NOT NULL,
    FOREIGN KEY (account_id) REFERENCES accounts(id)
);
CREATE INDEX idx_drift_items_account ON drift_items(account_id);
CREATE INDEX idx_drift_items_status ON drift_items(status);
```

### 5. Store Selector (`cmd/cloudpam/`)

Add `selectDriftStore()` to `store_default.go`, `store_sqlite.go`, `store_postgres.go` — following the exact same pattern as `selectRecommendationStore`.

### 6. API Endpoints (`internal/api/drift_handlers.go`)

New `DriftServer` struct (same pattern as RecommendationServer):

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1/drift/detect` | Run drift detection for an account |
| `GET` | `/api/v1/drift` | List drift items with filters |
| `GET` | `/api/v1/drift/{id}` | Get single drift item |
| `POST` | `/api/v1/drift/{id}/resolve` | Mark as resolved |
| `POST` | `/api/v1/drift/{id}/ignore` | Mark as ignored |

### 7. Wire Up in `main.go`

Initialize `DriftService`, `DriftStore`, `DriftServer` and register protected routes — same pattern as recommendations subsystem.

### 8. Frontend (`ui/src/pages/DriftPage.tsx`)

- Table view of drift items with severity badges (critical=red, warning=yellow, info=blue)
- Filter by type, severity, status, account
- Detail view showing the resource and pool side-by-side
- Resolve/Ignore action buttons
- Summary cards at top: total open, critical count, warning count
- Add route + sidebar entry

### 9. Frontend Hooks (`ui/src/hooks/useDrift.ts`)

API hooks for drift endpoints following existing patterns.

### 10. Tests

- `internal/planning/drift_test.go` — unit tests for the detection engine
- `internal/api/drift_handlers_test.go` — HTTP handler tests
- `ui/src/__tests__/DriftPage.test.tsx` — frontend component tests

## File Summary

| File | Action |
|------|--------|
| `internal/domain/drift.go` | Create |
| `internal/planning/drift.go` | Create |
| `internal/planning/drift_test.go` | Create |
| `internal/storage/drift.go` | Create |
| `internal/storage/drift_memory.go` | Create |
| `internal/storage/sqlite/drift.go` | Create |
| `migrations/0018_drift_items.sql` | Create |
| `internal/api/drift_handlers.go` | Create |
| `internal/api/drift_handlers_test.go` | Create |
| `cmd/cloudpam/main.go` | Edit — wire drift subsystem |
| `cmd/cloudpam/store_default.go` | Edit — add selectDriftStore |
| `cmd/cloudpam/store_sqlite.go` | Edit — add selectDriftStore |
| `cmd/cloudpam/store_postgres.go` | Edit — add selectDriftStore |
| `ui/src/pages/DriftPage.tsx` | Create |
| `ui/src/hooks/useDrift.ts` | Create |
| `ui/src/api/types.ts` | Edit — add drift types |
| `ui/src/api/client.ts` | Edit — add drift API functions |
| `ui/src/App.tsx` | Edit — add drift route |
| `ui/src/components/Sidebar.tsx` (or Layout) | Edit — add drift nav link |
