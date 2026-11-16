-- 0005_api_tokens.up.sql
--
-- Personal API tokens let scripts and CI call the JSON API without a browser
-- session. Only the SHA-256 hash of a token is stored: the plaintext is shown
-- once at creation and is unrecoverable afterwards, the same contract as a
-- GitHub PAT. `prefix` keeps a short non-secret lead-in so a user can tell two
-- tokens apart in the list view without revealing either one.
CREATE TABLE api_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL DEFAULT '',
    token_hash   TEXT NOT NULL UNIQUE,
    prefix       TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

CREATE INDEX api_tokens_user_id_idx ON api_tokens (user_id);
