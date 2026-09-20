package codexidentity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
)

type MappingScope struct {
	Secret          []byte
	AuthScope       string
	CredentialScope string
	Algorithm       string
	KeyEpoch        string
}

type Mapper struct {
	secret          []byte
	authScope       string
	credentialScope string
	algorithm       string
	keyEpoch        string
}

func NewMapper(scope MappingScope) (*Mapper, error) {
	if len(scope.Secret) < 32 {
		return nil, errors.New("codex identity mapping secret must contain at least 32 bytes")
	}
	if strings.TrimSpace(scope.AuthScope) == "" || strings.TrimSpace(scope.CredentialScope) == "" {
		return nil, errors.New("codex identity auth and credential scopes are required")
	}
	algorithm := strings.TrimSpace(scope.Algorithm)
	if algorithm == "" {
		algorithm = "hmac-sha256-v1"
	}
	epoch := strings.TrimSpace(scope.KeyEpoch)
	if epoch == "" {
		epoch = "epoch-1"
	}
	return &Mapper{
		secret:          append([]byte(nil), scope.Secret...),
		authScope:       strings.TrimSpace(scope.AuthScope),
		credentialScope: strings.TrimSpace(scope.CredentialScope),
		algorithm:       algorithm,
		keyEpoch:        epoch,
	}, nil
}

func (m *Mapper) Map(domain, raw string) (string, error) {
	if m == nil {
		return "", errors.New("codex identity mapper is nil")
	}
	domain = strings.TrimSpace(domain)
	raw = strings.TrimSpace(raw)
	if domain == "" || raw == "" {
		return "", errors.New("codex identity mapping domain and raw value are required")
	}
	digest := m.digest(domain, raw)
	return encodeMappedValue(raw, digest), nil
}

func (m *Mapper) digest(domain, raw string) [32]byte {
	mac := hmac.New(sha256.New, m.secret)
	writeFramed(mac, "sub2api-codex-r2")
	writeFramed(mac, m.algorithm)
	writeFramed(mac, m.keyEpoch)
	writeFramed(mac, m.authScope)
	writeFramed(mac, m.credentialScope)
	writeFramed(mac, domain)
	writeFramed(mac, raw)
	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}

type framedWriter interface {
	Write([]byte) (int, error)
}

func writeFramed(w framedWriter, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = w.Write(length[:])
	_, _ = w.Write([]byte(value))
}

func encodeMappedValue(raw string, digest [32]byte) string {
	if parsed, err := uuid.Parse(raw); err == nil {
		mapped := digest[:16]
		version := parsed.Version()
		if version == 7 {
			copy(mapped[:6], parsed[:6])
		}
		if version < 1 || version > 8 {
			version = 4
		}
		mapped[6] = (mapped[6] & 0x0f) | byte(version<<4)
		mapped[8] = (mapped[8] & 0x3f) | 0x80
		mappedUUID, _ := uuid.FromBytes(mapped)
		return mappedUUID.String()
	}
	lower := strings.ToLower(raw)
	if (len(lower) == 16 || len(lower) == 32) && isHex(lower) {
		return hex.EncodeToString(digest[:len(lower)/2])
	}
	mapped := digest[:16]
	mapped[6] = (mapped[6] & 0x0f) | 0x40
	mapped[8] = (mapped[8] & 0x3f) | 0x80
	mappedUUID, _ := uuid.FromBytes(mapped)
	return mappedUUID.String()
}

func isHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}
