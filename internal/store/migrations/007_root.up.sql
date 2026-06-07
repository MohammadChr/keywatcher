ALTER TABLE users ADD COLUMN IF NOT EXISTS is_root BOOLEAN NOT NULL DEFAULT false;
UPDATE users SET is_root = true
WHERE id = (SELECT id FROM users ORDER BY created_at ASC LIMIT 1);
