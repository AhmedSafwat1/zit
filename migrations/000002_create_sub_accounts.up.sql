-- parent -> child relation between two users. The parent row may be a
-- placeholder (users.synced_at IS NULL) created when the child synced first.
CREATE TABLE sub_accounts (
    parent_user_id bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    child_user_id  bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sub_accounts_pkey    PRIMARY KEY (parent_user_id, child_user_id),
    CONSTRAINT sub_accounts_no_self CHECK (parent_user_id <> child_user_id)
);

CREATE INDEX sub_accounts_child_idx ON sub_accounts (child_user_id);
