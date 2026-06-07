package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"keywatcher/internal/model"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgres(ctx context.Context, dsn string) (*PostgresStore, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("store.NewPostgres parse config: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2

	// Add statement timeout (Fix 7.2)
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = make(map[string]string)
	}
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "30000"  // 30 seconds
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "60000" // 60 seconds

	// NEVER log the DSN — it contains credentials (Fix 7.1)
	log.Info().Msg("connecting to database")

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store.NewPostgres connect: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

// ── Assets ───────────────────────────────────────────────────────

func (s *PostgresStore) CreateAsset(ctx context.Context, a *model.Asset) error {
	tags, _ := json.Marshal(a.Tags)
	meta, _ := json.Marshal(a.Metadata)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO assets (id, name, type, expires_at, description, tags, metadata, created_by)
		VALUES (@id, @name, @type, @expires_at, @description, @tags, @metadata, @created_by)`,
		pgx.NamedArgs{
			"id": a.ID, "name": a.Name, "type": string(a.Type),
			"expires_at": a.ExpiresAt, "description": a.Description,
			"tags": tags, "metadata": meta, "created_by": a.CreatedBy,
		})
	if err != nil {
		return fmt.Errorf("store.CreateAsset: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetAsset(ctx context.Context, id uuid.UUID) (*model.Asset, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT a.id, a.name, a.type, a.expires_at, a.description, a.tags, a.metadata,
		       a.created_at, a.updated_at, a.created_by, a.deleted_at,
		       CASE WHEN s.asset_id IS NOT NULL
		            AND (s.expires_at IS NULL OR s.expires_at > NOW())
		            THEN true ELSE false END as is_silenced
		FROM assets a
		LEFT JOIN alert_silences s ON s.asset_id = a.id
		WHERE a.id = @id AND a.deleted_at IS NULL`,
		pgx.NamedArgs{"id": id})
	return scanAsset(row)
}

func (s *PostgresStore) ListAssets(ctx context.Context, f AssetFilter) ([]*model.Asset, int, error) {
	if f.Limit == 0 {
		f.Limit = 50
	}
	if f.Page == 0 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.Limit

	where := "deleted_at IS NULL"
	args := pgx.NamedArgs{}

	if f.Type != "" {
		where += " AND type = @type"
		args["type"] = f.Type
	}
	switch f.Status {
	case "expiring":
		where += " AND expires_at > NOW() AND expires_at <= NOW() + INTERVAL '30 days'"
	case "expired":
		where += " AND expires_at < NOW()"
	case "valid":
		where += " AND expires_at > NOW() + INTERVAL '30 days'"
	}
	if f.TagKey != "" && f.TagValue != "" {
		where += " AND tags->@tag_key = @tag_value"
		args["tag_key"] = f.TagKey
		args["tag_value"] = `"` + f.TagValue + `"`
	}

	var total int
	err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM assets WHERE "+where, args).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("store.ListAssets count: %w", err)
	}

	args["limit"] = f.Limit
	args["offset"] = offset
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.name, a.type, a.expires_at, a.description, a.tags, a.metadata,
		       a.created_at, a.updated_at, a.created_by, a.deleted_at,
		       CASE WHEN s.asset_id IS NOT NULL
		            AND (s.expires_at IS NULL OR s.expires_at > NOW())
		            THEN true ELSE false END as is_silenced
		FROM assets a
		LEFT JOIN alert_silences s ON s.asset_id = a.id
		WHERE `+where+`
		ORDER BY a.expires_at ASC LIMIT @limit OFFSET @offset`, args)
	if err != nil {
		return nil, 0, fmt.Errorf("store.ListAssets query: %w", err)
	}
	defer rows.Close()

	var assets []*model.Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, 0, err
		}
		assets = append(assets, a)
	}
	return assets, total, nil
}

func (s *PostgresStore) UpdateAsset(ctx context.Context, a *model.Asset) error {
	tags, _ := json.Marshal(a.Tags)
	meta, _ := json.Marshal(a.Metadata)
	_, err := s.pool.Exec(ctx, `
		UPDATE assets SET name=@name, type=@type, expires_at=@expires_at,
			description=@description, tags=@tags, metadata=@metadata, updated_at=NOW()
		WHERE id=@id AND deleted_at IS NULL`,
		pgx.NamedArgs{
			"id": a.ID, "name": a.Name, "type": string(a.Type),
			"expires_at": a.ExpiresAt, "description": a.Description,
			"tags": tags, "metadata": meta,
		})
	if err != nil {
		return fmt.Errorf("store.UpdateAsset: %w", err)
	}
	return nil
}

func (s *PostgresStore) DeleteAsset(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE assets SET deleted_at=NOW() WHERE id=@id AND deleted_at IS NULL",
		pgx.NamedArgs{"id": id})
	if err != nil {
		return fmt.Errorf("store.DeleteAsset: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListAllActive(ctx context.Context) ([]*model.Asset, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.name, a.type, a.expires_at, a.description, a.tags, a.metadata,
		       a.created_at, a.updated_at, a.created_by, a.deleted_at,
		       CASE WHEN s.asset_id IS NOT NULL
		            AND (s.expires_at IS NULL OR s.expires_at > NOW())
		            THEN true ELSE false END as is_silenced
		FROM assets a
		LEFT JOIN alert_silences s ON s.asset_id = a.id
		WHERE a.deleted_at IS NULL ORDER BY a.expires_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("store.ListAllActive: %w", err)
	}
	defer rows.Close()
	var assets []*model.Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, a)
	}
	return assets, nil
}

// ── Users ────────────────────────────────────────────────────────

func (s *PostgresStore) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	row := s.pool.QueryRow(ctx,
		"SELECT id,username,email,password_hash,auth_method,role,is_root,created_at,last_login FROM users WHERE username=@u",
		pgx.NamedArgs{"u": username})
	return scanUser(row)
}

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	row := s.pool.QueryRow(ctx,
		"SELECT id,username,email,password_hash,auth_method,role,is_root,created_at,last_login FROM users WHERE email=@e",
		pgx.NamedArgs{"e": email})
	return scanUser(row)
}

func (s *PostgresStore) GetUserByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	row := s.pool.QueryRow(ctx,
		"SELECT id,username,email,password_hash,auth_method,role,is_root,created_at,last_login FROM users WHERE id=@id",
		pgx.NamedArgs{"id": id})
	return scanUser(row)
}

func (s *PostgresStore) CreateUser(ctx context.Context, u *model.User) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id,username,email,password_hash,auth_method,role,is_root)
		VALUES (@id,@username,@email,@password_hash,@auth_method,@role,@is_root)
		ON CONFLICT (email) DO UPDATE SET last_login=NOW()`,
		pgx.NamedArgs{
			"id": u.ID, "username": u.Username, "email": u.Email,
			"password_hash": u.PasswordHash, "auth_method": string(u.AuthMethod),
			"role": string(u.Role), "is_root": u.IsRoot,
		})
	if err != nil {
		return fmt.Errorf("store.CreateUser: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, "UPDATE users SET last_login=NOW() WHERE id=@id", pgx.NamedArgs{"id": id})
	return err
}

func (s *PostgresStore) UpdateUserRole(ctx context.Context, id uuid.UUID, role model.UserRole) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE users SET role=@role WHERE id=@id",
		pgx.NamedArgs{"role": string(role), "id": id})
	return err
}

func (s *PostgresStore) UpdateUser(ctx context.Context, id uuid.UUID, username, email string) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE users SET username=@username, email=@email WHERE id=@id",
		pgx.NamedArgs{"username": username, "email": email, "id": id})
	if err != nil {
		return fmt.Errorf("store.UpdateUser: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpdatePassword(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE users SET password_hash=@hash WHERE id=@id",
		pgx.NamedArgs{"hash": hash, "id": id})
	if err != nil {
		return fmt.Errorf("store.UpdatePassword: %w", err)
	}
	return nil
}

func (s *PostgresStore) DeleteUser(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		"DELETE FROM users WHERE id=@id",
		pgx.NamedArgs{"id": id})
	if err != nil {
		return fmt.Errorf("store.DeleteUser: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListUsers(ctx context.Context) ([]*model.User, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT id,username,email,password_hash,auth_method,role,is_root,created_at,last_login FROM users ORDER BY created_at ASC")
	if err != nil {
		return nil, fmt.Errorf("store.ListUsers: %w", err)
	}
	defer rows.Close()
	var users []*model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil { return nil, err }
		users = append(users, u)
	}
	return users, nil
}

// ── Notification Log ─────────────────────────────────────────────

func (s *PostgresStore) WasNotifiedRecently(ctx context.Context, assetID uuid.UUID, daysLeft int, since time.Duration) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM notification_log
			WHERE asset_id=@id AND days_left=@dl AND sent_at > NOW() - @since::interval
		)`, pgx.NamedArgs{"id": assetID, "dl": daysLeft, "since": since.String()}).Scan(&exists)
	return exists, err
}

func (s *PostgresStore) LogNotification(ctx context.Context, assetID uuid.UUID, daysLeft int, channel string) error {
	_, err := s.pool.Exec(ctx,
		"INSERT INTO notification_log (asset_id,days_left,channel) VALUES (@id,@dl,@ch)",
		pgx.NamedArgs{"id": assetID, "dl": daysLeft, "ch": channel})
	return err
}

// ── Scanners ─────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanAsset(row scanner) (*model.Asset, error) {
	var a model.Asset
	var tagsRaw, metaRaw []byte
	err := row.Scan(
		&a.ID, &a.Name, &a.Type, &a.ExpiresAt, &a.Description,
		&tagsRaw, &metaRaw, &a.CreatedAt, &a.UpdatedAt, &a.CreatedBy, &a.DeletedAt, &a.IsSilenced,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scanAsset: %w", err)
	}
	_ = json.Unmarshal(tagsRaw, &a.Tags)
	_ = json.Unmarshal(metaRaw, &a.Metadata)
	return &a, nil
}

func scanUser(row scanner) (*model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.AuthMethod, &u.Role, &u.IsRoot, &u.CreatedAt, &u.LastLogin)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scanUser: %w", err)
	}
	return &u, nil
}

// ── Setup ────────────────────────────────────────────────────────

func (s *PostgresStore) IsSetupCompleted(ctx context.Context) (bool, error) {
	var value string
	err := s.pool.QueryRow(ctx,
		"SELECT value FROM app_settings WHERE key = 'setup_completed'").Scan(&value)
	if err != nil {
		return false, fmt.Errorf("store.IsSetupCompleted: %w", err)
	}
	return value == "true", nil
}

func (s *PostgresStore) CompleteSetup(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE app_settings SET value = 'true' WHERE key = 'setup_completed'")
	return err
}

// ── Settings ──────────────────────────────────────────────────────

func (s *PostgresStore) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx,
		"SELECT value FROM app_settings WHERE key=@key",
		pgx.NamedArgs{"key": key}).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (s *PostgresStore) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.pool.Exec(ctx,
		"INSERT INTO app_settings (key,value) VALUES (@key,@value) ON CONFLICT (key) DO UPDATE SET value=@value",
		pgx.NamedArgs{"key": key, "value": value})
	return err
}

func (s *PostgresStore) GetAllSettings(ctx context.Context, prefix string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT key,value FROM app_settings WHERE key LIKE @prefix",
		pgx.NamedArgs{"prefix": prefix + "%"})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, nil
}

func (s *PostgresStore) SilenceAsset(ctx context.Context, assetID uuid.UUID, silencedBy, note string, expiresAt *time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO alert_silences (asset_id, silenced_by, note, expires_at)
		VALUES (@asset_id, @silenced_by, @note, @expires_at)
		ON CONFLICT (asset_id) DO UPDATE
			SET silenced_by=@silenced_by, silenced_at=NOW(), note=@note, expires_at=@expires_at`,
		pgx.NamedArgs{
			"asset_id": assetID, "silenced_by": silencedBy,
			"note": note, "expires_at": expiresAt,
		})
	return err
}

func (s *PostgresStore) UnsilenceAsset(ctx context.Context, assetID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		"DELETE FROM alert_silences WHERE asset_id=@id",
		pgx.NamedArgs{"id": assetID})
	return err
}

func (s *PostgresStore) IsAssetSilenced(ctx context.Context, assetID uuid.UUID) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM alert_silences
			WHERE asset_id=@id
			AND (expires_at IS NULL OR expires_at > NOW())
		)`, pgx.NamedArgs{"id": assetID}).Scan(&exists)
	return exists, err
}

func (s *PostgresStore) ListSilences(ctx context.Context) ([]*model.Silence, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, asset_id, silenced_by, silenced_at, expires_at, note
		FROM alert_silences
		WHERE expires_at IS NULL OR expires_at > NOW()
		ORDER BY silenced_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var silences []*model.Silence
	for rows.Next() {
		var si model.Silence
		if err := rows.Scan(&si.ID, &si.AssetID, &si.SilencedBy,
			&si.SilencedAt, &si.ExpiresAt, &si.Note); err != nil {
			return nil, err
		}
		silences = append(silences, &si)
	}
	return silences, nil
}
