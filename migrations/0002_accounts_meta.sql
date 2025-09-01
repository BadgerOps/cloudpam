-- Extend accounts with optional metadata fields for analytics filtering
ALTER TABLE accounts ADD COLUMN platform TEXT NULL;
ALTER TABLE accounts ADD COLUMN tier TEXT NULL;
ALTER TABLE accounts ADD COLUMN environment TEXT NULL;
-- regions stored as JSON array of strings
ALTER TABLE accounts ADD COLUMN regions TEXT NULL;
