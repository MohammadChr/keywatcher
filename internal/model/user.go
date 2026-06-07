package model

import (
	"time"

	"github.com/google/uuid"
)

type AuthMethod string
type UserRole string

const (
	AuthMethodLocal AuthMethod = "local"
	AuthMethodOIDC  AuthMethod = "oidc"
	AuthMethodLDAP  AuthMethod = "ldap"
	RoleAdmin       UserRole   = "admin"
	RoleViewer      UserRole   = "viewer"
)

type User struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	Username     string     `json:"username" db:"username"`
	Email        string     `json:"email" db:"email"`
	PasswordHash *string    `json:"-" db:"password_hash"`
	AuthMethod   AuthMethod `json:"auth_method" db:"auth_method"`
	Role         UserRole   `json:"role" db:"role"`
	IsRoot       bool       `json:"is_root" db:"is_root"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	LastLogin    *time.Time `json:"last_login,omitempty" db:"last_login"`
}
