package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"sync"
	"vaultwatch/internal/model"
	"vaultwatch/internal/store"
	"github.com/google/uuid"
	ldap "github.com/go-ldap/ldap/v3"
)

type LDAPConfig struct {
	URL          string
	BindDN       string
	BindPassword string
	BaseDN       string
	UserFilter   string // e.g. "(uid=%s)"
}

type LDAPAuthenticator struct {
	cfg  LDAPConfig
	mu   sync.Mutex
	conn *ldap.Conn
}

func NewLDAPAuthenticator(cfg LDAPConfig) *LDAPAuthenticator {
	return &LDAPAuthenticator{cfg: cfg}
}

func (l *LDAPAuthenticator) connect() error {
	var conn *ldap.Conn
	var err error
	if strings.HasPrefix(l.cfg.URL, "ldaps://") {
		conn, err = ldap.DialURL(l.cfg.URL, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: false}))
	} else {
		conn, err = ldap.DialURL(l.cfg.URL)
		if err == nil {
			err = conn.StartTLS(&tls.Config{InsecureSkipVerify: false})
		}
	}
	if err != nil {
		return fmt.Errorf("ldap.connect: %w", err)
	}
	l.conn = conn
	return nil
}

func (l *LDAPAuthenticator) Authenticate(ctx context.Context, s store.Store, username, password string) (*model.User, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn == nil {
		if err := l.connect(); err != nil {
			return nil, err
		}
	}

	// Bind with service account
	if err := l.conn.Bind(l.cfg.BindDN, l.cfg.BindPassword); err != nil {
		l.conn = nil
		return nil, fmt.Errorf("ldap.Authenticate service bind: %w", err)
	}

	// Search for user
	filter := fmt.Sprintf(l.cfg.UserFilter, ldap.EscapeFilter(username))
	result, err := l.conn.Search(&ldap.SearchRequest{
		BaseDN: l.cfg.BaseDN,
		Scope:  ldap.ScopeWholeSubtree,
		Filter: filter,
		Attributes: []string{"dn", "mail", "cn"},
	})
	if err != nil || len(result.Entries) == 0 {
		return nil, fmt.Errorf("ldap.Authenticate: user not found")
	}

	userDN := result.Entries[0].DN
	email  := result.Entries[0].GetAttributeValue("mail")

	// Verify user password
	if err := l.conn.Bind(userDN, password); err != nil {
		return nil, fmt.Errorf("ldap.Authenticate: invalid credentials")
	}

	// Re-bind as service account for future ops
	_ = l.conn.Bind(l.cfg.BindDN, l.cfg.BindPassword)

	// Upsert user in local DB
	user := &model.User{
		ID:         uuid.New(),
		Username:   username,
		Email:      email,
		AuthMethod: model.AuthMethodLDAP,
	}
	if err := s.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("ldap.Authenticate upsert: %w", err)
	}
	return user, nil
}
