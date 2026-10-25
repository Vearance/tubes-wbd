BEGIN;

INSERT INTO users (
    username,
    password_hash,
    is_admin,
    created_at,
    updated_at
)
SELECT
    username,
    '$2a$10$o8lu/YDq9Igp5MhcXDlnMOsBpY3EWnlhP9Z0mabUBIXZh8yoAspmW',
    FALSE,
    NOW(),
    NOW()
FROM (
    VALUES ('rudolf'), ('airgroove'), ('specialweek'), ('goldship'), ('tachyon')
) AS seed(username)
ON CONFLICT (username) DO NOTHING;

INSERT INTO "groups" (
    name,
    created_at,
    updated_at
)
VALUES
    ('Student Council', NOW(), NOW()),
    ('Team Spica', NOW(), NOW())
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
FROM (
    VALUES
        ('rudolf', 'Student Council'),
        ('airgroove', 'Student Council'),
        ('specialweek', 'Team Spica'),
        ('goldship', 'Team Spica')
) AS seed(username, group_name)
JOIN users ON users.username = seed.username
JOIN "groups" AS platform_group ON platform_group.name = seed.group_name
ON CONFLICT (user_id, group_id) DO NOTHING;

COMMIT;
