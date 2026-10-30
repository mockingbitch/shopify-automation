-- name: NotifyMembers :exec
-- Notifies members holding one of the roles, plus the listed users. An
-- unread notification with the same dedupe key is not repeated.
INSERT INTO notifications (id, org_id, user_id, kind, title, body, link, dedupe_key, created_at)
SELECT gen_random_uuid(), m.org_id, m.user_id, @kind, @title, @body, @link, @dedupe_key, @created_at
FROM organization_members m
WHERE m.org_id = @org_id AND (m.role_key = ANY(@roles::text[]) OR m.user_id = ANY(@user_ids::uuid[]))
ON CONFLICT (org_id, user_id, dedupe_key) WHERE read_at IS NULL DO NOTHING;
