CREATE TABLE users (
    id            bigserial    PRIMARY KEY,
    sub           varchar(64)  NOT NULL,
    -- Everything below is NULL until the user authenticates at least once.
    -- A row can be created ahead of that as the parent of a sub-account
    -- (see sub_accounts), holding only its sub.
    email         varchar(320) NULL,
    nickname      varchar(255) NULL,
    -- Full snapshot of the last user.sync payload (roles, main_account,
    -- sub_account_of, raw Zitadel claims). Everything else is derived from it.
    snapshot      jsonb        NULL,
    -- sha256 of the identity-relevant fields; the upsert skips the write when unchanged.
    identity_hash char(64)     NULL,
    -- NULL = never synced (parent placeholder). Drives the stale guard.
    synced_at     timestamptz  NULL,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    updated_at    timestamptz  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_sub_key ON users (sub);
CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email)) WHERE email IS NOT NULL;
