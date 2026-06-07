INSERT INTO app_settings (key, value) VALUES ('app_logo', '')
ON CONFLICT (key) DO NOTHING;
