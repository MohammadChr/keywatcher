-- Reuse app_settings table from migration 003
INSERT INTO app_settings (key, value) VALUES
    ('auth_oidc_enabled', 'false'),
    ('auth_ldap_enabled', 'false'),
    ('auth_oidc_issuer', ''),
    ('auth_oidc_client_id', ''),
    ('auth_oidc_client_secret', ''),
    ('auth_ldap_url', ''),
    ('auth_ldap_bind_dn', ''),
    ('auth_ldap_bind_password', ''),
    ('auth_ldap_base_dn', ''),
    ('auth_ldap_user_filter', '(uid=%s)')
ON CONFLICT (key) DO NOTHING;
