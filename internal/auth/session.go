package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"vaultwatch/internal/model"
	"vaultwatch/internal/store"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	UserID     uuid.UUID          `json:"uid"`
	Email      string             `json:"email"`
	AuthMethod model.AuthMethod   `json:"auth_method"`
	Role       model.UserRole     `json:"role"`
	jwt.RegisteredClaims
}

func IssueToken(user *model.User, secret string, ttl time.Duration) (string, error) {
	claims := Claims{
		UserID:     user.ID,
		Email:      user.Email,
		AuthMethod: user.AuthMethod,
		Role:       user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("auth.IssueToken: %w", err)
	}
	return signed, nil
}

func ValidateToken(tokenStr, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth.ValidateToken: %w", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("auth.ValidateToken: invalid claims")
	}
	return claims, nil
}

type contextKey string
const ClaimsKey contextKey = "claims"

func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var tokenStr string

			// Try Authorization header first
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				tokenStr = strings.TrimPrefix(h, "Bearer ")
			}
			// Fall back to cookie
			if tokenStr == "" {
				if c, err := r.Cookie("session"); err == nil {
					tokenStr = c.Value
				}
			}

			if tokenStr == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			claims, err := ValidateToken(tokenStr, secret)
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), ClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetClaims(r *http.Request) *Claims {
	v := r.Context().Value(ClaimsKey)
	if v == nil { return nil }
	c, _ := v.(*Claims)
	return c
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetClaims(r)
		if claims == nil || claims.Role != model.RoleAdmin {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"forbidden","message":"admin access required"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSetupComplete redirects to setup if setup is not done yet.
// Apply this to ALL routes except /setup/* and /healthz and /readyz and /metrics
func RequireSetupComplete(s store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip check for setup routes themselves
			if strings.HasPrefix(r.URL.Path, "/setup") ||
				r.URL.Path == "/healthz" ||
				r.URL.Path == "/readyz" ||
				r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}
			done, err := s.IsSetupCompleted(r.Context())
			if err != nil || !done {
				// API requests get JSON, browser requests get redirect
				if strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusServiceUnavailable)
					w.Write([]byte(`{"error":"setup_required","redirect":"/setup"}`))
					return
				}
				http.Redirect(w, r, "/setup", http.StatusFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
