package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInitLogDBMigratesErrorAlertsWhenUsingMainDB(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	originalDB := DB
	originalLogDB := LOG_DB
	originalIsMasterNode := common.IsMasterNode
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.IsMasterNode = originalIsMasterNode
	})
	t.Setenv("LOG_SQL_DSN", "")
	DB = db
	LOG_DB = nil
	common.IsMasterNode = true

	require.NoError(t, InitLogDB())
	require.Same(t, DB, LOG_DB)
	require.True(t, LOG_DB.Migrator().HasTable(&ErrorAlert{}))
}
