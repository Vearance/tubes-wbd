-- Run this after the backend has completed GORM AutoMigrate.
-- Username: user
-- Password: password
-- The user is a member of group 1, which intentionally has no database access.

BEGIN;

INSERT INTO users (
    username,
    password_hash,
    is_admin,
    created_at,
    updated_at
)
VALUES (
    'user',
    '$2a$10$o8lu/YDq9Igp5MhcXDlnMOsBpY3EWnlhP9Z0mabUBIXZh8yoAspmW',
    FALSE,
    NOW(),
    NOW()
)
ON CONFLICT (username) DO NOTHING;

INSERT INTO "groups" (
    name,
    created_at,
    updated_at
)
VALUES (
    'group 1',
    NOW(),
    NOW()
)
ON CONFLICT (name) DO NOTHING;

INSERT INTO user_groups (
    user_id,
    group_id,
    created_at
)
SELECT
    users.id,
    platform_group.id,
    NOW()
FROM users
CROSS JOIN "groups" AS platform_group
WHERE users.username = 'user'
  AND platform_group.name = 'group 1'
ON CONFLICT (user_id, group_id) DO NOTHING;

-- No rows are inserted into group_database_permissions on purpose.

COMMIT;
