package service

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCreateDoubaoVideo2AccessKeyUsesOfficialShapeAndEncryptedSecret(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/doubao-video2-access-key.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.DoubaoVideo2AccessKey{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		_ = sqlDB.Close()
	})

	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	t.Setenv(upstreamCredentialKeysEnv, `{"1":"`+key+`"}`)
	t.Setenv(upstreamCredentialActiveVersionEnv, "1")

	created, err := CreateDoubaoVideo2AccessKey(42, "material-uploader")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(created.Key.AccessKeyID, "AKLT"))
	require.Len(t, created.Key.AccessKeyID, 24)
	require.Len(t, created.SecretAccessKey, 40)
	require.NotContains(t, created.Key.SecretCiphertext, created.SecretAccessKey)
	require.NotEmpty(t, created.Key.SecretNonce)

	decrypted, err := DecryptDoubaoVideo2AccessKeySecret(created.Key)
	require.NoError(t, err)
	require.Equal(t, created.SecretAccessKey, decrypted)
}
