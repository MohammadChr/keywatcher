DROP TABLE IF EXISTS alert_silences;
DELETE FROM app_settings WHERE key LIKE 'alert_%';
