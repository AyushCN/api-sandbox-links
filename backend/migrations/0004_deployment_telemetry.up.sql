-- 0004_deployment_telemetry.up.sql
--
-- Adds structured build telemetry to deployments: builder type, image build
-- duration, confirmed cache hit indicator, and resulting Docker image size.

ALTER TABLE deployments
    ADD COLUMN builder TEXT,
    ADD COLUMN build_duration_ms BIGINT,
    ADD COLUMN cache_hit BOOLEAN,
    ADD COLUMN image_size_bytes BIGINT;

COMMENT ON COLUMN deployments.builder IS 'Build system used (e.g. nixpacks, railpack)';
COMMENT ON COLUMN deployments.build_duration_ms IS 'Image compile/build duration in milliseconds';
COMMENT ON COLUMN deployments.cache_hit IS 'Whether the build confirmed a cache hit from builder logs';
COMMENT ON COLUMN deployments.image_size_bytes IS 'Inspected container image size in bytes';
