CREATE TABLE IF NOT EXISTS recommendations (
    id TEXT PRIMARY KEY,
    pool_id INTEGER NOT NULL REFERENCES pools(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('allocation','compliance','consolidation','resize','reclaim')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','applied','dismissed')),
    priority TEXT NOT NULL DEFAULT 'medium' CHECK (priority IN ('high','medium','low')),
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    suggested_cidr TEXT,
    rule_id TEXT,
    score INTEGER NOT NULL DEFAULT 0,
    metadata TEXT NOT NULL DEFAULT '{}',
    dismiss_reason TEXT,
    applied_pool_id INTEGER REFERENCES pools(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_recommendations_pool_status ON recommendations(pool_id, status);
CREATE INDEX IF NOT EXISTS idx_recommendations_type ON recommendations(type);
CREATE INDEX IF NOT EXISTS idx_recommendations_status ON recommendations(status);
