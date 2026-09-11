package service

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/google/uuid"
)

// CodexFingerprintSeedExtraKey is the system-managed per-account namespace
// used to derive stable Codex installation/session/thread identifiers.
// It is deliberately kept in account Extra so no schema migration is needed.
const CodexFingerprintSeedExtraKey = "codex_fingerprint_seed"

const codexFingerprintModeExtraKey = "codex_fingerprint_mode"

// CodexFingerprintSeed returns a canonical, non-nil UUID seed from account
// extra. Invalid or user-supplied values are treated as absent.
func CodexFingerprintSeed(extra string) (string, bool) {
	values := map[string]any{}
	if strings.TrimSpace(extra) == "" || common.UnmarshalJsonStr(extra, &values) != nil {
		return "", false
	}
	raw, ok := values[CodexFingerprintSeedExtraKey].(string)
	if !ok {
		return "", false
	}
	raw = strings.TrimSpace(raw)
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed == uuid.Nil || raw != parsed.String() {
		return "", false
	}
	return raw, true
}

func codexFingerprintModeRequiresSeed(extra string) bool {
	values := map[string]any{}
	if strings.TrimSpace(extra) == "" || common.UnmarshalJsonStr(extra, &values) != nil {
		return false
	}
	mode, _ := values[codexFingerprintModeExtraKey].(string)
	if strings.TrimSpace(mode) == "" {
		mode = system_setting.GetCodexSetting().FingerprintDefaultMode
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "device", "session", "full":
		return true
	default:
		return false
	}
}

func cloneExtraWithoutCodexSeed(extra string) (map[string]any, error) {
	values := map[string]any{}
	if strings.TrimSpace(extra) != "" && strings.TrimSpace(extra) != "{}" {
		if err := common.UnmarshalJsonStr(extra, &values); err != nil {
			return nil, err
		}
	}
	delete(values, CodexFingerprintSeedExtraKey)
	return values, nil
}

func encodeCodexExtra(values map[string]any) (string, error) {
	if values == nil {
		values = map[string]any{}
	}
	encoded, err := common.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// PrepareCodexFingerprintExtraForCreate strips any client-provided seed and
// mints a fresh seed only for explicitly enabled OpenAI OAuth-like accounts.
func PrepareCodexFingerprintExtraForCreate(account *model.UpstreamAccount) error {
	if account == nil {
		return errors.New("upstream account is required")
	}
	values, err := cloneExtraWithoutCodexSeed(account.Extra)
	if err != nil {
		return err
	}
	if account.Platform == constant.UpstreamPlatformOpenAI &&
		(account.Type == constant.UpstreamAccountTypeOAuth || account.Type == constant.UpstreamAccountTypeSetupToken) &&
		codexFingerprintModeRequiresSeed(account.Extra) {
		values[CodexFingerprintSeedExtraKey] = uuid.NewString()
	}
	account.Extra, err = encodeCodexExtra(values)
	return err
}

// PrepareCodexFingerprintExtraForUpdate strips a client-provided seed while
// preserving the existing seed. A missing seed is created when the desired
// account explicitly enables convergence, matching Sub2API's lifecycle.
func PrepareCodexFingerprintExtraForUpdate(current, desired *model.UpstreamAccount) error {
	if desired == nil {
		return errors.New("upstream account is required")
	}
	values, err := cloneExtraWithoutCodexSeed(desired.Extra)
	if err != nil {
		return err
	}
	if desired.Platform == constant.UpstreamPlatformOpenAI &&
		(desired.Type == constant.UpstreamAccountTypeOAuth || desired.Type == constant.UpstreamAccountTypeSetupToken) {
		if current != nil && current.Platform == constant.UpstreamPlatformOpenAI &&
			(current.Type == constant.UpstreamAccountTypeOAuth || current.Type == constant.UpstreamAccountTypeSetupToken) {
			if seed, ok := CodexFingerprintSeed(current.Extra); ok {
				values[CodexFingerprintSeedExtraKey] = seed
			} else if codexFingerprintModeRequiresSeed(desired.Extra) {
				values[CodexFingerprintSeedExtraKey] = uuid.NewString()
			}
		} else if codexFingerprintModeRequiresSeed(desired.Extra) {
			values[CodexFingerprintSeedExtraKey] = uuid.NewString()
		}
	}
	desired.Extra, err = encodeCodexExtra(values)
	return err
}

// RedactCodexFingerprintSeed removes the system seed from an administrative
// account view. The backend preserves it on subsequent updates, so the seed
// never needs to be sent to or rendered by the browser.
func RedactCodexFingerprintSeed(extra string) string {
	values, err := cloneExtraWithoutCodexSeed(extra)
	if err != nil {
		return extra
	}
	encoded, err := encodeCodexExtra(values)
	if err != nil {
		return extra
	}
	return encoded
}

// EnsureCodexFingerprintSeedsBackfill upgrades existing explicitly configured
// OAuth accounts. It is idempotent and uses an optimistic extra-value guard so
// multiple application nodes can run it safely during a rolling deployment.
func EnsureCodexFingerprintSeedsBackfill() (int, error) {
	if model.DB == nil {
		return 0, nil
	}
	var accounts []model.UpstreamAccount
	if err := model.DB.Where("platform = ? AND type = ?", constant.UpstreamPlatformOpenAI, constant.UpstreamAccountTypeOAuth).Find(&accounts).Error; err != nil {
		return 0, err
	}
	updated := 0
	for i := range accounts {
		account := &accounts[i]
		if _, ok := CodexFingerprintSeed(account.Extra); ok || !codexFingerprintModeRequiresSeed(account.Extra) {
			continue
		}
		originalExtra := account.Extra
		if err := PrepareCodexFingerprintExtraForUpdate(account, account); err != nil {
			return updated, err
		}
		result := model.DB.Model(&model.UpstreamAccount{}).
			Where("id = ? AND extra = ?", account.Id, originalExtra).
			Update("extra", account.Extra)
		if result.Error != nil {
			return updated, result.Error
		}
		if result.RowsAffected == 1 {
			updated++
		}
	}
	return updated, nil
}
