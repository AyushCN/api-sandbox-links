-- 0004_analysis_overrides.up.sql
--
-- Preflight analysis produces a start command and a service decision that must
-- survive from creation to every later redeploy/fork. Persisting them on the
-- sandbox keeps redeploys deterministic instead of re-deriving a different plan
-- from a newer commit, and lets the UI explain what was chosen.
ALTER TABLE sandboxes
    ADD COLUMN start_command TEXT,
    ADD COLUMN services_override JSONB;
