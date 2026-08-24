package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	upstreamCredentialKeysEnv          = "UPSTREAM_ACCOUNT_CREDENTIAL_KEYS"
	upstreamCredentialActiveVersionEnv = "UPSTREAM_ACCOUNT_ACTIVE_KEY_VERSION"
	upstreamCredentialNonceSize        = 12
)

type UpstreamCredentialEnvelope struct {
	Ciphertext string
	Nonce      string
	KeyVersion int
}

type UpstreamCredentialKeyring struct {
	activeVersion int
	keys          map[int][]byte
	plaintext     bool
}

func LoadUpstreamCredentialKeyringFromEnv() (*UpstreamCredentialKeyring, error) {
	rawKeys := os.Getenv(upstreamCredentialKeysEnv)
	if strings.TrimSpace(rawKeys) == "" {
		return &UpstreamCredentialKeyring{plaintext: true}, nil
	}
	return ParseUpstreamCredentialKeyring(
		rawKeys,
		os.Getenv(upstreamCredentialActiveVersionEnv),
	)
}

func ParseUpstreamCredentialKeyring(rawKeys string, rawActiveVersion string) (*UpstreamCredentialKeyring, error) {
	if strings.TrimSpace(rawKeys) == "" {
		return nil, errors.New("upstream credential keyring is not configured")
	}
	var encodedKeys map[string]string
	if err := common.UnmarshalJsonStr(rawKeys, &encodedKeys); err != nil {
		return nil, errors.New("upstream credential keyring must be a JSON object")
	}
	activeVersion, err := strconv.Atoi(strings.TrimSpace(rawActiveVersion))
	if err != nil || activeVersion <= 0 {
		return nil, errors.New("upstream credential active key version must be a positive integer")
	}
	keys := make(map[int][]byte, len(encodedKeys))
	for rawVersion, encodedKey := range encodedKeys {
		version, parseErr := strconv.Atoi(strings.TrimSpace(rawVersion))
		if parseErr != nil || version <= 0 {
			return nil, errors.New("upstream credential key version must be a positive integer")
		}
		key, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
		if decodeErr != nil || len(key) != 32 {
			return nil, fmt.Errorf("upstream credential key version %d must be a base64-encoded 32-byte key", version)
		}
		keys[version] = key
	}
	if _, ok := keys[activeVersion]; !ok {
		return nil, errors.New("upstream credential active key version is missing from keyring")
	}
	return &UpstreamCredentialKeyring{activeVersion: activeVersion, keys: keys}, nil
}

func (keyring *UpstreamCredentialKeyring) ActiveVersion() int {
	if keyring == nil {
		return 0
	}
	return keyring.activeVersion
}

func (keyring *UpstreamCredentialKeyring) IsEncrypted() bool {
	return keyring != nil && !keyring.plaintext && keyring.activeVersion > 0
}

func (keyring *UpstreamCredentialKeyring) EncryptJSON(recordKind string, recordId int, credentialVersion int64, value any) (*UpstreamCredentialEnvelope, error) {
	plaintext, err := common.Marshal(value)
	if err != nil {
		return nil, errors.New("failed to encode upstream credential")
	}
	return keyring.Encrypt(recordKind, recordId, credentialVersion, plaintext)
}

func (keyring *UpstreamCredentialKeyring) Encrypt(recordKind string, recordId int, credentialVersion int64, plaintext []byte) (*UpstreamCredentialEnvelope, error) {
	if keyring == nil || recordId <= 0 || credentialVersion <= 0 || strings.TrimSpace(recordKind) == "" {
		return nil, errors.New("invalid upstream credential encryption context")
	}
	if keyring.plaintext {
		return &UpstreamCredentialEnvelope{Ciphertext: string(plaintext)}, nil
	}
	key, ok := keyring.keys[keyring.activeVersion]
	if !ok {
		return nil, errors.New("active upstream credential key is unavailable")
	}
	aead, err := newUpstreamCredentialAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, upstreamCredentialNonceSize)
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, errors.New("failed to generate upstream credential nonce")
	}
	aad := upstreamCredentialAAD(recordKind, recordId, credentialVersion, keyring.activeVersion)
	ciphertext := aead.Seal(nil, nonce, plaintext, aad)
	return &UpstreamCredentialEnvelope{
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		KeyVersion: keyring.activeVersion,
	}, nil
}

func (keyring *UpstreamCredentialKeyring) DecryptJSON(envelope UpstreamCredentialEnvelope, recordKind string, recordId int, credentialVersion int64, value any) error {
	plaintext, err := keyring.Decrypt(envelope, recordKind, recordId, credentialVersion)
	if err != nil {
		return err
	}
	if err := common.Unmarshal(plaintext, value); err != nil {
		return errors.New("decrypted upstream credential is invalid")
	}
	return nil
}

func (keyring *UpstreamCredentialKeyring) Decrypt(envelope UpstreamCredentialEnvelope, recordKind string, recordId int, credentialVersion int64) ([]byte, error) {
	if keyring == nil || recordId <= 0 || credentialVersion <= 0 || strings.TrimSpace(recordKind) == "" {
		return nil, errors.New("invalid upstream credential decryption context")
	}
	if envelope.KeyVersion == 0 {
		if envelope.Nonce != "" {
			return nil, errors.New("plaintext upstream credential nonce must be empty")
		}
		return []byte(envelope.Ciphertext), nil
	}
	key, ok := keyring.keys[envelope.KeyVersion]
	if !ok {
		return nil, errors.New("upstream credential key version is unavailable")
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil || len(nonce) != upstreamCredentialNonceSize {
		return nil, errors.New("upstream credential nonce is invalid")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, errors.New("upstream credential ciphertext is invalid")
	}
	aead, err := newUpstreamCredentialAEAD(key)
	if err != nil {
		return nil, err
	}
	aad := upstreamCredentialAAD(recordKind, recordId, credentialVersion, envelope.KeyVersion)
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, errors.New("upstream credential authentication failed")
	}
	return plaintext, nil
}

func newUpstreamCredentialAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("failed to initialize upstream credential cipher")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("failed to initialize upstream credential AEAD")
	}
	return aead, nil
}

func upstreamCredentialAAD(recordKind string, recordId int, credentialVersion int64, keyVersion int) []byte {
	return []byte(fmt.Sprintf("starnexus:upstream-credential:%s:%d:%d:%d", strings.TrimSpace(recordKind), recordId, credentialVersion, keyVersion))
}
