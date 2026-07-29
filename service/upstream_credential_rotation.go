package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type UpstreamCredentialRotationReport struct {
	ActiveKeyVersion int   `json:"active_key_version"`
	ScannedAccounts  int64 `json:"scanned_accounts"`
	RotatedAccounts  int64 `json:"rotated_accounts"`
	ScannedProxies   int64 `json:"scanned_proxies"`
	RotatedProxies   int64 `json:"rotated_proxies"`
	ScannedSessions  int64 `json:"scanned_sessions"`
	RotatedSessions  int64 `json:"rotated_sessions"`
	RemainingRecords int64 `json:"remaining_records"`
}

func InspectUpstreamCredentialRotation(ctx context.Context) (*UpstreamCredentialRotationReport, error) {
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return nil, err
	}
	report := &UpstreamCredentialRotationReport{ActiveKeyVersion: keyring.ActiveVersion()}
	counts := []struct {
		model  any
		column string
		value  *int64
	}{
		{&model.UpstreamAccount{}, "credential_key_version", &report.ScannedAccounts},
		{&model.UpstreamOAuthSession{}, "verifier_key_version", &report.ScannedSessions},
	}
	for _, item := range counts {
		if err := model.DB.WithContext(ctx).Model(item.model).Where(item.column+" <> ?", keyring.ActiveVersion()).Count(item.value).Error; err != nil {
			return nil, err
		}
		report.RemainingRecords += *item.value
	}
	if err := model.DB.WithContext(ctx).Model(&model.UpstreamProxy{}).
		Where("auth_ciphertext <> ? AND auth_nonce <> ? AND auth_key_version > ? AND auth_key_version <> ?", "", "", 0, keyring.ActiveVersion()).
		Count(&report.ScannedProxies).Error; err != nil {
		return nil, err
	}
	report.RemainingRecords += report.ScannedProxies
	return report, nil
}

func VerifyUpstreamCredentials(ctx context.Context, batchSize int) (*UpstreamCredentialRotationReport, error) {
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return nil, err
	}
	batchSize = normalizeCredentialRotationBatchSize(batchSize)
	report := &UpstreamCredentialRotationReport{ActiveKeyVersion: keyring.ActiveVersion()}
	if err := scanUpstreamCredentialRecords(ctx, batchSize,
		func(account model.UpstreamAccount) error {
			report.ScannedAccounts++
			var payload map[string]any
			return keyring.DecryptJSON(accountCredentialEnvelope(account), upstreamAccountCredentialRecordKind, account.Id, account.CredentialVersion, &payload)
		},
		func(proxy model.UpstreamProxy) error {
			report.ScannedProxies++
			var payload UpstreamProxyAuthInput
			return keyring.DecryptJSON(proxyCredentialEnvelope(proxy), upstreamProxyAuthRecordKind, proxy.Id, proxy.AuthCredentialVersion, &payload)
		},
		func(session model.UpstreamOAuthSession) error {
			report.ScannedSessions++
			var payload map[string]string
			return keyring.DecryptJSON(oauthSessionCredentialEnvelope(session), upstreamOAuthSessionRecordKind, session.Id, session.VerifierVersion, &payload)
		},
	); err != nil {
		return nil, err
	}
	inspection, err := InspectUpstreamCredentialRotation(ctx)
	if err != nil {
		return nil, err
	}
	report.RemainingRecords = inspection.RemainingRecords
	return report, nil
}

func RotateUpstreamCredentials(ctx context.Context, batchSize int) (*UpstreamCredentialRotationReport, error) {
	keyring, err := LoadUpstreamCredentialKeyringFromEnv()
	if err != nil {
		return nil, err
	}
	batchSize = normalizeCredentialRotationBatchSize(batchSize)
	report := &UpstreamCredentialRotationReport{ActiveKeyVersion: keyring.ActiveVersion()}
	if err := scanUpstreamCredentialRecords(ctx, batchSize,
		func(account model.UpstreamAccount) error {
			report.ScannedAccounts++
			if account.CredentialKeyVersion == keyring.ActiveVersion() {
				return nil
			}
			var payload map[string]any
			if err := keyring.DecryptJSON(accountCredentialEnvelope(account), upstreamAccountCredentialRecordKind, account.Id, account.CredentialVersion, &payload); err != nil {
				return fmt.Errorf("decrypt upstream account %d: %w", account.Id, err)
			}
			newVersion := account.CredentialVersion + 1
			envelope, err := keyring.EncryptJSON(upstreamAccountCredentialRecordKind, account.Id, newVersion, payload)
			if err != nil {
				return err
			}
			result := model.DB.WithContext(ctx).Model(&model.UpstreamAccount{}).
				Where("id = ? AND credential_version = ? AND credential_key_version = ?", account.Id, account.CredentialVersion, account.CredentialKeyVersion).
				Updates(map[string]any{
					"credential_ciphertext": envelope.Ciphertext, "credential_nonce": envelope.Nonce,
					"credential_key_version": envelope.KeyVersion, "credential_version": newVersion,
					"updated_at": common.GetTimestamp(),
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("upstream account %d changed during credential rotation", account.Id)
			}
			report.RotatedAccounts++
			return nil
		},
		func(proxy model.UpstreamProxy) error {
			report.ScannedProxies++
			if proxy.AuthKeyVersion == keyring.ActiveVersion() {
				return nil
			}
			var payload UpstreamProxyAuthInput
			if err := keyring.DecryptJSON(proxyCredentialEnvelope(proxy), upstreamProxyAuthRecordKind, proxy.Id, proxy.AuthCredentialVersion, &payload); err != nil {
				return fmt.Errorf("decrypt upstream proxy %d: %w", proxy.Id, err)
			}
			newVersion := proxy.AuthCredentialVersion + 1
			envelope, err := keyring.EncryptJSON(upstreamProxyAuthRecordKind, proxy.Id, newVersion, payload)
			if err != nil {
				return err
			}
			result := model.DB.WithContext(ctx).Model(&model.UpstreamProxy{}).
				Where("id = ? AND auth_credential_version = ? AND auth_key_version = ?", proxy.Id, proxy.AuthCredentialVersion, proxy.AuthKeyVersion).
				Updates(map[string]any{
					"auth_ciphertext": envelope.Ciphertext, "auth_nonce": envelope.Nonce,
					"auth_key_version": envelope.KeyVersion, "auth_credential_version": newVersion,
					"updated_at": common.GetTimestamp(),
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("upstream proxy %d changed during credential rotation", proxy.Id)
			}
			report.RotatedProxies++
			return nil
		},
		func(session model.UpstreamOAuthSession) error {
			report.ScannedSessions++
			if session.VerifierKeyVersion == keyring.ActiveVersion() {
				return nil
			}
			var payload map[string]string
			if err := keyring.DecryptJSON(oauthSessionCredentialEnvelope(session), upstreamOAuthSessionRecordKind, session.Id, session.VerifierVersion, &payload); err != nil {
				return fmt.Errorf("decrypt upstream OAuth session %d: %w", session.Id, err)
			}
			newVersion := session.VerifierVersion + 1
			envelope, err := keyring.EncryptJSON(upstreamOAuthSessionRecordKind, session.Id, newVersion, payload)
			if err != nil {
				return err
			}
			result := model.DB.WithContext(ctx).Model(&model.UpstreamOAuthSession{}).
				Where("id = ? AND verifier_version = ? AND verifier_key_version = ?", session.Id, session.VerifierVersion, session.VerifierKeyVersion).
				Updates(map[string]any{
					"verifier_ciphertext": envelope.Ciphertext, "verifier_nonce": envelope.Nonce,
					"verifier_key_version": envelope.KeyVersion, "verifier_version": newVersion,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("upstream OAuth session %d changed during credential rotation", session.Id)
			}
			report.RotatedSessions++
			return nil
		},
	); err != nil {
		return nil, err
	}
	inspection, err := InspectUpstreamCredentialRotation(ctx)
	if err != nil {
		return nil, err
	}
	report.RemainingRecords = inspection.RemainingRecords
	return report, nil
}

func scanUpstreamCredentialRecords(
	ctx context.Context,
	batchSize int,
	accountFn func(model.UpstreamAccount) error,
	proxyFn func(model.UpstreamProxy) error,
	sessionFn func(model.UpstreamOAuthSession) error,
) error {
	if model.DB == nil {
		return errors.New("database is not initialized")
	}
	if err := scanCredentialTable(ctx, batchSize, func(lastId int, limit int) ([]model.UpstreamAccount, error) {
		var records []model.UpstreamAccount
		err := model.DB.WithContext(ctx).Where("id > ?", lastId).Order("id ASC").Limit(limit).Find(&records).Error
		return records, err
	}, func(record model.UpstreamAccount) int { return record.Id }, accountFn); err != nil {
		return err
	}
	if err := scanCredentialTable(ctx, batchSize, func(lastId int, limit int) ([]model.UpstreamProxy, error) {
		var records []model.UpstreamProxy
		err := model.DB.WithContext(ctx).
			Where("id > ? AND auth_ciphertext <> ? AND auth_nonce <> ? AND auth_key_version > ?", lastId, "", "", 0).
			Order("id ASC").Limit(limit).Find(&records).Error
		return records, err
	}, func(record model.UpstreamProxy) int { return record.Id }, proxyFn); err != nil {
		return err
	}
	return scanCredentialTable(ctx, batchSize, func(lastId int, limit int) ([]model.UpstreamOAuthSession, error) {
		var records []model.UpstreamOAuthSession
		err := model.DB.WithContext(ctx).Where("id > ?", lastId).Order("id ASC").Limit(limit).Find(&records).Error
		return records, err
	}, func(record model.UpstreamOAuthSession) int { return record.Id }, sessionFn)
}

func scanCredentialTable[T any](
	ctx context.Context,
	batchSize int,
	load func(lastId int, limit int) ([]T, error),
	id func(T) int,
	visit func(T) error,
) error {
	lastId := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		records, err := load(lastId, batchSize)
		if err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		for _, record := range records {
			if err := visit(record); err != nil {
				return err
			}
			lastId = id(record)
		}
		if len(records) < batchSize {
			return nil
		}
	}
}

func normalizeCredentialRotationBatchSize(value int) int {
	if value < 1 {
		return 100
	}
	if value > 1000 {
		return 1000
	}
	return value
}

func accountCredentialEnvelope(account model.UpstreamAccount) UpstreamCredentialEnvelope {
	return UpstreamCredentialEnvelope{Ciphertext: account.CredentialCiphertext, Nonce: account.CredentialNonce, KeyVersion: account.CredentialKeyVersion}
}

func proxyCredentialEnvelope(proxy model.UpstreamProxy) UpstreamCredentialEnvelope {
	return UpstreamCredentialEnvelope{Ciphertext: proxy.AuthCiphertext, Nonce: proxy.AuthNonce, KeyVersion: proxy.AuthKeyVersion}
}

func oauthSessionCredentialEnvelope(session model.UpstreamOAuthSession) UpstreamCredentialEnvelope {
	return UpstreamCredentialEnvelope{Ciphertext: session.VerifierCiphertext, Nonce: session.VerifierNonce, KeyVersion: session.VerifierKeyVersion}
}
