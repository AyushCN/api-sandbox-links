-- 0004_deployment_telemetry.down.sql
ALTER TABLE deployments
    DROP COLUMN IF EXISTS builder,
    DROP COLUMN IF EXISTS build_duration_ms,
    DROP COLUMN IF EXISTS cache_hit,
    DROP COLUMN IF EXISTS image_size_bytes;
