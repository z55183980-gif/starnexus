package model

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAdminLogCursorPreservesFiltersAndHistory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/cursor.db"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	require.NoError(t, db.AutoMigrate(&Log{}))
	original := LOG_DB
	LOG_DB = db
	defer func() { LOG_DB = original }()
	for _, log := range []Log{
		{Id: 1, Type: LogTypeConsume, CreatedAt: 100},
		{Id: 2, Type: LogTypeTopup, CreatedAt: 100},
		{Id: 3, Type: LogTypeConsume, CreatedAt: 105},
		{Id: 4, Type: LogTypeConsume, CreatedAt: 101},
		{Id: 5, Type: LogTypeConsume, CreatedAt: 300},
		{Id: 6, Type: LogTypeRefund, CreatedAt: 100},
	} {
		require.NoError(t, db.Create(&log).Error)
	}
	query := func(offset int, option LogQueryOptions) ([]*Log, int64, error) {
		return GetAllLogs(LogTypeConsume, 90, 200, "", "", "", offset, 2, 0, "", "", "", nil, option)
	}
	first, total, err := query(0, LogQueryOptions{})
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	require.Equal(t, 4, first[0].Id)
	require.Equal(t, 3, first[1].Id)
	// An insertion above the cursor cannot cause duplicate rows on the next page.
	require.NoError(t, db.Create(&Log{Id: 7, Type: LogTypeConsume, CreatedAt: 110}).Error)
	second, total, err := query(100000, LogQueryOptions{BeforeID: 3})
	require.NoError(t, err)
	require.EqualValues(t, 4, total)
	require.Len(t, second, 1)
	require.Equal(t, 1, second[0].Id)
	// Count-free mode never invokes the count callback, preserving filters.
	noCount, _, err := query(0, LogQueryOptions{SkipCount: true, Count: func(*gorm.DB) (int64, error) {
		return 0, errors.New("count must not run")
	}})
	require.NoError(t, err)
	require.Len(t, noCount, 2)
	require.Equal(t, 7, noCount[0].Id)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = query(0, LogQueryOptions{Context: ctx})
	require.Error(t, err)
	var remaining int64
	require.NoError(t, db.Model(&Log{}).Count(&remaining).Error)
	require.EqualValues(t, 7, remaining)
}
