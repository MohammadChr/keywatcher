INSERT INTO app_settings (key, value) VALUES
    ('alert_template_warning',  '⚠️ *{asset_name}* is expiring in *{days_left} days*\nType: {asset_type}\nExpires: {expires_at}\nEnvironment: {env}'),
    ('alert_template_critical', '🚨 *{asset_name}* expires in *{days_left} days* — ACTION REQUIRED\nType: {asset_type}\nExpires: {expires_at}\nEnvironment: {env}'),
    ('alert_template_expired',  '☠️ *{asset_name}* has EXPIRED\nType: {asset_type}\nExpired: {expires_at}\nEnvironment: {env}')
ON CONFLICT (key) DO NOTHING;
