package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const doubaoVideo2AccessKeyRecordKind = "doubao-video2-access-key"

type DoubaoVideo2AccessKeyCreated struct {
	Key             *model.DoubaoVideo2AccessKey `json:"key"`
	SecretAccessKey string                       `json:"secret_access_key"`
}

func CreateDoubaoVideo2AccessKey(userID int, name string) (*DoubaoVideo2AccessKeyCreated, error) {
	name = strings.TrimSpace(name)
	if userID <= 0 || name == "" || len(name) > 64 {
		return nil, errors.New("access key name must be between 1 and 64 characters")
	}
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return nil, err
	}
	if !keyring.IsEncrypted() {
		return nil, errors.New("encrypted credential storage is required; configure UPSTREAM_ACCOUNT_CREDENTIAL_KEYS and UPSTREAM_ACCOUNT_ACTIVE_KEY_VERSION")
	}
	akSuffix, err := common.GenerateRandomCharsKey(20)
	if err != nil {
		return nil, fmt.Errorf("generate Access Key ID: %w", err)
	}
	secret, err := common.GenerateRandomCharsKey(40)
	if err != nil {
		return nil, fmt.Errorf("generate Secret Access Key: %w", err)
	}
	key := &model.DoubaoVideo2AccessKey{
		UserID: userID, Name: name, AccessKeyID: "AKLT" + akSuffix,
		Status: model.DoubaoVideo2AccessKeyStatusActive, CredentialVersion: 1,
		SecretCiphertext: "pending",
	}
	if err := model.CreateDoubaoVideo2AccessKey(key); err != nil {
		return nil, err
	}
	envelope, err := keyring.Encrypt(doubaoVideo2AccessKeyRecordKind, int(key.ID), key.CredentialVersion, []byte(secret))
	if err != nil {
		_ = model.DeleteDoubaoVideo2AccessKey(key.ID, userID)
		return nil, err
	}
	updates := map[string]any{
		"secret_ciphertext":  envelope.Ciphertext,
		"secret_nonce":       envelope.Nonce,
		"secret_key_version": envelope.KeyVersion,
	}
	if err := model.UpdateDoubaoVideo2AccessKey(key.ID, userID, updates); err != nil {
		_ = model.DeleteDoubaoVideo2AccessKey(key.ID, userID)
		return nil, err
	}
	key.SecretCiphertext = envelope.Ciphertext
	key.SecretNonce = envelope.Nonce
	key.SecretKeyVersion = envelope.KeyVersion
	return &DoubaoVideo2AccessKeyCreated{Key: key, SecretAccessKey: secret}, nil
}

func DecryptDoubaoVideo2AccessKeySecret(key *model.DoubaoVideo2AccessKey) (string, error) {
	if key == nil || key.ID <= 0 || key.CredentialVersion <= 0 {
		return "", errors.New("invalid Doubao video access key")
	}
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return "", err
	}
	plaintext, err := keyring.Decrypt(UpstreamCredentialEnvelope{
		Ciphertext: key.SecretCiphertext,
		Nonce:      key.SecretNonce,
		KeyVersion: key.SecretKeyVersion,
	}, doubaoVideo2AccessKeyRecordKind, int(key.ID), key.CredentialVersion)
	if err != nil {
		return "", err
	}
	secret := strings.TrimSpace(string(plaintext))
	if secret == "" {
		return "", errors.New("Doubao video access key secret is empty")
	}
	return secret, nil
}
