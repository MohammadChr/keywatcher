package auth

import (
	"context"
	"fmt"
	"vaultwatch/internal/model"
	"vaultwatch/internal/store"
	"golang.org/x/crypto/bcrypt"
	"github.com/google/uuid"
)

const bcryptCost = 12

func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("auth.HashPassword: %w", err)
	}
	return string(b), nil
}

func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

func AuthenticateLocal(ctx context.Context, s store.Store, username, password string) (*model.User, error) {
	user, err := s.GetUserByUsername(ctx, username)
	if err != nil {
		// Still do a dummy bcrypt to prevent timing attacks (Fix 3.4)
		bcrypt.CompareHashAndPassword([]byte("$2a$12$dummy.hash.to.prevent.timing.attack"), []byte(password))
		return nil, fmt.Errorf("invalid credentials")
	}
	if user == nil || user.PasswordHash == nil {
		// Still do a dummy bcrypt to prevent timing attacks (Fix 3.4)
		bcrypt.CompareHashAndPassword([]byte("$2a$12$dummy.hash.to.prevent.timing.attack"), []byte(password))
		return nil, fmt.Errorf("invalid credentials")
	}
	if !VerifyPassword(*user.PasswordHash, password) {
		return nil, fmt.Errorf("invalid credentials")
	}
	return user, nil
}

func CreateLocalUser(ctx context.Context, s store.Store, username, email, password string, role model.UserRole) error {
	if username == "" || email == "" || password == "" {
		return fmt.Errorf("auth.CreateLocalUser: all fields required")
	}
	if len(password) < 8 {
		return fmt.Errorf("auth.CreateLocalUser: password must be at least 8 characters")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.CreateUser(ctx, &model.User{
		ID:           uuid.New(),
		Username:     username,
		Email:        email,
		PasswordHash: &hash,
		AuthMethod:   model.AuthMethodLocal,
		Role:         role,
	})
}
