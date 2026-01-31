-- Enhanced Pool Model: Add type, status, source, description, tags, and updated_at columns

-- Add new columns for enhanced pool model
ALTER TABLE pools ADD COLUMN type TEXT NOT NULL DEFAULT 'subnet';
ALTER TABLE pools ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE pools ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE pools ADD COLUMN description TEXT DEFAULT '';
ALTER TABLE pools ADD COLUMN tags TEXT DEFAULT '{}';
ALTER TABLE pools ADD COLUMN updated_at TIMESTAMP DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'));

-- Create index for type queries (useful for filtering by pool type)
CREATE INDEX IF NOT EXISTS idx_pools_type ON pools(type);

-- Create index for status queries (useful for filtering by status)
CREATE INDEX IF NOT EXISTS idx_pools_status ON pools(status);
