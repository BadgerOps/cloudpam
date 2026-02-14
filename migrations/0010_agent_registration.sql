-- Migration: Agent Registration
-- Description: Add agent approval status and registration tracking columns

-- Add status column to discovery_agents for approval workflow
ALTER TABLE discovery_agents ADD COLUMN status TEXT NOT NULL DEFAULT 'approved';
-- status: pending_approval, approved, rejected

-- Add registration tracking columns
ALTER TABLE discovery_agents ADD COLUMN registered_at TEXT;
ALTER TABLE discovery_agents ADD COLUMN approved_at TEXT;
ALTER TABLE discovery_agents ADD COLUMN approved_by TEXT;

CREATE INDEX IF NOT EXISTS idx_discovery_agents_status ON discovery_agents(status);
