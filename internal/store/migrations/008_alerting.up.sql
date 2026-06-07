-- Alerting global settings (reuse app_settings table)
INSERT INTO app_settings (key, value) VALUES
    ('alert_warning_days',     '30'),
    ('alert_critical_days',    '7'),
    ('alert_interval',         '1h'),
    ('alert_slack_webhook',    ''),
    ('alert_slack_token',      ''),
    ('alert_slack_channel',    ''),
    ('alert_mattermost_webhook',''),
    ('alert_mattermost_token', ''),
    ('alert_mattermost_channel',''),
    ('alert_webhook_url',      ''),
    ('alert_telegram_token',   ''),
    ('alert_telegram_chat_id', '')
ON CONFLICT (key) DO NOTHING;

-- Silence table
CREATE TABLE IF NOT EXISTS alert_silences (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id   UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    silenced_by TEXT NOT NULL,
    silenced_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ,          -- NULL = silenced forever until unsilenced
    note        TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_silence_asset ON alert_silences(asset_id);
