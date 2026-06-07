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
	GetUserByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	CreateUser(ctx context.Context, u *model.User) error
	UpdateLastLogin(ctx context.Context, id uuid.UUID) error
	UpdateUserRole(ctx context.Context, id uuid.UUID, role model.UserRole) error
	UpdateUser(ctx context.Context, id uuid.UUID, username, email string) error
	UpdatePassword(ctx context.Context, id uuid.UUID, hash string) error
	DeleteUser(ctx context.Context, id uuid.UUID) error
	ListUsers(ctx context.Context) ([]*model.User, error)

	// Notification log
	WasNotifiedRecently(ctx context.Context, assetID uuid.UUID, daysLeft int, since time.Duration) (bool, error)
	LogNotification(ctx context.Context, assetID uuid.UUID, daysLeft int, channel string) error

	// Settings
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
	GetAllSettings(ctx context.Context, prefix string) (map[string]string, error)

	// Silence
	SilenceAsset(ctx context.Context, assetID uuid.UUID, silencedBy string, note string, expiresAt *time.Time) error
	UnsilenceAsset(ctx context.Context, assetID uuid.UUID) error
	IsAssetSilenced(ctx context.Context, assetID uuid.UUID) (bool, error)
	ListSilences(ctx context.Context) ([]*model.Silence, error)

	// Setup
	IsSetupCompleted(ctx context.Context) (bool, error)
	CompleteSetup(ctx context.Context) error

	// Health
	Ping(ctx context.Context) error
	Close()
}
