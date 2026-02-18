-- Utilization snapshots for tracking pool usage over time.
-- Enables growth projections and capacity planning.
CREATE TABLE IF NOT EXISTS utilization_snapshots (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    pool_id     INTEGER NOT NULL REFERENCES pools(id),
    total_ips   INTEGER NOT NULL,
    used_ips    INTEGER NOT NULL,
    available_ips INTEGER NOT NULL,
    utilization REAL NOT NULL,          -- 0-100 percentage
    child_count INTEGER NOT NULL DEFAULT 0,
    captured_at TEXT NOT NULL,           -- RFC3339 timestamp
    UNIQUE(pool_id, captured_at)
);

CREATE INDEX IF NOT EXISTS idx_utilization_snapshots_pool_id ON utilization_snapshots(pool_id);
CREATE INDEX IF NOT EXISTS idx_utilization_snapshots_captured_at ON utilization_snapshots(captured_at);
