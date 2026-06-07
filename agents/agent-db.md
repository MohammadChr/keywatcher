# Agent: Database

You own the entire database layer. Read CLAUDE.md first.
Use pgx/v5 exclusively. No ORM. Raw SQL only. Named parameters only.

## Task 1 — Migrations

Create internal/store/migrations/001_init.up.sql:
```sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TYPE asset_type AS ENUM ('certificate','token','api_key','secret','custom');

CREATE TABLE assets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    type        asset_type NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    tags        JSONB NOT NULL DEFAULT '{}',
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  TEXT NOT NULL,
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX idx_assets_expires_at  ON assets(expires_at) WHERE deleted_at IS NULL;
CREATE INDEX idx_assets_type        ON assets(type)       WHERE deleted_at IS NULL;
CREATE INDEX idx_assets_deleted_at  ON assets(deleted_at);
CREATE INDEX idx_assets_tags        ON assets USING GIN(tags);

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT UNIQUE NOT NULL,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT,
    auth_method   TEXT NOT NULL CHECK (auth_method IN ('local','oidc','ldap')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login    TIMESTAMPTZ
);

CREATE TABLE notification_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id   UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    days_left  INT NOT NULL,
    sent_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    channel    TEXT NOT NULL
);
CREATE INDEX idx_notif_asset_sent ON notification_log(asset_id, sent_at DESC);
```

Create internal/store/migrations/001_init.down.sql:
```sql
DROP TABLE IF EXISTS notification_log;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS assets;
DROP TYPE  IF EXISTS asset_type;
```

## Task 2 — Store Interface

Create internal/store/store.go:
```go
package store

import (
    "context"
    "time"
    "vaultwatch/internal/model"
    "github.com/google/uuid"
)

type AssetFilter struct {
    Type      string
    Status    string // valid | expiring | expired
    TagKey    string
    TagValue  string
    Page      int
    Limit     int
}

type Store interface {
    // Assets
    CreateAsset(ctx context.Context, a *model.Asset) error
    GetAsset(ctx context.Context, id uuid.UUID) (*model.Asset, error)
    ListAssets(ctx context.Context, f AssetFilter) ([]*model.Asset, int, error)
    UpdateAsset(ctx context.Context, a *model.Asset) error
    DeleteAsset(ctx context.Context, id uuid.UUID) error
    ListAllActive(ctx context.Context) ([]*model.Asset, error)

    // Users
    GetUserByUsername(ctx context.Context, username string) (*model.User, error)
    GetUserByEmail(ctx context.Context, email string) (*model.User, error)
    CreateUser(ctx context.Context, u *model.User) error
    UpdateLastLogin(ctx context.Context, id uuid.UUID) error

    // Notification log
    WasNotifiedRecently(ctx context.Context, assetID uuid.UUID, daysLeft int, since time.Duration) (bool, error)
    LogNotification(ctx context.Context, assetID uuid.UUID, daysLeft int, channel string) error

    // Health
    Ping(ctx context.Context) error
    Close()
}
```

## Task 3 — Postgres Implementation

Create internal/store/postgres.go:
```go
package store

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "vaultwatch/internal/model"
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
        SELECT id, name, type, expires_at, description, tags, metadata,
               created_at, updated_at, created_by, deleted_at
        FROM assets WHERE id = @id AND deleted_at IS NULL`,
        pgx.NamedArgs{"id": id})
    return scanAsset(row)
}

func (s *PostgresStore) ListAssets(ctx context.Context, f AssetFilter) ([]*model.Asset, int, error) {
    if f.Limit == 0 { f.Limit = 50 }
    if f.Page == 0  { f.Page = 1 }
    offset := (f.Page - 1) * f.Limit

    where := "deleted_at IS NULL"
    args  := pgx.NamedArgs{}

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
        args["tag_key"]   = f.TagKey
        args["tag_value"] = `"` + f.TagValue + `"`
    }

    var total int
    err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM assets WHERE "+where, args).Scan(&total)
    if err != nil {
        return nil, 0, fmt.Errorf("store.ListAssets count: %w", err)
    }

    args["limit"]  = f.Limit
    args["offset"] = offset
    rows, err := s.pool.Query(ctx, `
        SELECT id, name, type, expires_at, description, tags, metadata,
               created_at, updated_at, created_by, deleted_at
        FROM assets WHERE `+where+`
        ORDER BY expires_at ASC LIMIT @limit OFFSET @offset`, args)
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
        SELECT id, name, type, expires_at, description, tags, metadata,
               created_at, updated_at, created_by, deleted_at
        FROM assets WHERE deleted_at IS NULL ORDER BY expires_at ASC`)
    if err != nil {
        return nil, fmt.Errorf("store.ListAllActive: %w", err)
    }
    defer rows.Close()
    var assets []*model.Asset
    for rows.Next() {
        a, err := scanAsset(rows)
        if err != nil { return nil, err }
        assets = append(assets, a)
    }
    return assets, nil
}

// ── Users ────────────────────────────────────────────────────────

func (s *PostgresStore) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
    row := s.pool.QueryRow(ctx,
        "SELECT id,username,email,password_hash,auth_method,created_at,last_login FROM users WHERE username=@u",
        pgx.NamedArgs{"u": username})
    return scanUser(row)
}

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
    row := s.pool.QueryRow(ctx,
        "SELECT id,username,email,password_hash,auth_method,created_at,last_login FROM users WHERE email=@e",
        pgx.NamedArgs{"e": email})
    return scanUser(row)
}

func (s *PostgresStore) CreateUser(ctx context.Context, u *model.User) error {
    if u.ID == uuid.Nil { u.ID = uuid.New() }
    _, err := s.pool.Exec(ctx, `
        INSERT INTO users (id,username,email,password_hash,auth_method)
        VALUES (@id,@username,@email,@password_hash,@auth_method)
        ON CONFLICT (email) DO UPDATE SET last_login=NOW()`,
        pgx.NamedArgs{
            "id": u.ID, "username": u.Username, "email": u.Email,
            "password_hash": u.PasswordHash, "auth_method": string(u.AuthMethod),
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
        &tagsRaw, &metaRaw, &a.CreatedAt, &a.UpdatedAt, &a.CreatedBy, &a.DeletedAt,
    )
    if err != nil {
        if err == pgx.ErrNoRows { return nil, nil }
        return nil, fmt.Errorf("scanAsset: %w", err)
    }
    _ = json.Unmarshal(tagsRaw, &a.Tags)
    _ = json.Unmarshal(metaRaw, &a.Metadata)
    return &a, nil
}

func scanUser(row scanner) (*model.User, error) {
    var u model.User
    err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.AuthMethod, &u.CreatedAt, &u.LastLogin)
    if err != nil {
        if err == pgx.ErrNoRows { return nil, nil }
        return nil, fmt.Errorf("scanUser: %w", err)
    }
    return &u, nil
}
```

## Final check
Run `go build ./...` — fix any errors before finishing.
