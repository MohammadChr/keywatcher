package model

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type AssetType string

const (
	AssetTypeCert   AssetType = "certificate"
	AssetTypeToken  AssetType = "token"
	AssetTypeAPIKey AssetType = "api_key"
	AssetTypeSecret AssetType = "secret"
	AssetTypeCustom AssetType = "custom"
)

func (t AssetType) Valid() bool {
	switch t {
	case AssetTypeCert, AssetTypeToken, AssetTypeAPIKey, AssetTypeSecret, AssetTypeCustom:
		return true
	}
	return false
}

type Asset struct {
	ID          uuid.UUID         `json:"id" db:"id"`
	Name        string            `json:"name" db:"name"`
	Type        AssetType         `json:"type" db:"type"`
	ExpiresAt   time.Time         `json:"expires_at" db:"expires_at"`
	Description string            `json:"description" db:"description"`
	Tags        map[string]string `json:"tags" db:"tags"`
	Metadata    map[string]any    `json:"metadata" db:"metadata"`
	CreatedAt   time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at" db:"updated_at"`
	CreatedBy   string            `json:"created_by" db:"created_by"`
	DeletedAt   *time.Time        `json:"deleted_at,omitempty" db:"deleted_at"`
	IsSilenced  bool              `json:"is_silenced" db:"is_silenced"`
}

func (a *Asset) DaysUntilExpiry() int {
	return int(time.Until(a.ExpiresAt).Hours() / 24)
}

func (a *Asset) Status() string {
	days := a.DaysUntilExpiry()
	if days < 0 {
		return "expired"
	}
	if days <= 30 {
		return "expiring"
	}
	return "valid"
}

type CertInfo struct {
	Subject     string
	Issuer      string
	SANs        []string
	ExpiresAt   time.Time
	Fingerprint string
}

func ParseCertificate(pemData string) (*CertInfo, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("model.ParseCertificate: invalid PEM data")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("model.ParseCertificate: %w", err)
	}
	fp := sha256.Sum256(cert.Raw)
	sans := cert.DNSNames
	for _, ip := range cert.IPAddresses {
		sans = append(sans, ip.String())
	}
	return &CertInfo{
		Subject:     cert.Subject.CommonName,
		Issuer:      cert.Issuer.CommonName,
		SANs:        sans,
		ExpiresAt:   cert.NotAfter,
		Fingerprint: fmt.Sprintf("%x", fp),
	}, nil
}

type Silence struct {
	ID         uuid.UUID  `json:"id"`
	AssetID    uuid.UUID  `json:"asset_id"`
	SilencedBy string     `json:"silenced_by"`
	SilencedAt time.Time  `json:"silenced_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Note       string     `json:"note"`
}
