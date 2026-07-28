package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type options struct {
	Mode      string
	DSN       string
	BatchSize int
	Timeout   time.Duration
}

func main() {
	opts := options{}
	flag.StringVar(&opts.Mode, "mode", "plan", "plan, rotate, or verify")
	flag.StringVar(&opts.DSN, "dsn", os.Getenv("SQL_DSN"), "StarNexus database DSN")
	flag.IntVar(&opts.BatchSize, "batch-size", 100, "records per scan batch")
	flag.DurationVar(&opts.Timeout, "timeout", 30*time.Minute, "overall command timeout")
	flag.Parse()

	db, err := openDatabase(opts.DSN)
	if err != nil {
		exitWithError(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		exitWithError(err)
	}
	defer sqlDB.Close()
	model.DB = db

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	var report *service.UpstreamCredentialRotationReport
	switch strings.ToLower(strings.TrimSpace(opts.Mode)) {
	case "plan":
		report, err = service.InspectUpstreamCredentialRotation(ctx)
	case "rotate":
		report, err = service.RotateUpstreamCredentials(ctx, opts.BatchSize)
	case "verify":
		report, err = service.VerifyUpstreamCredentials(ctx, opts.BatchSize)
	default:
		err = errors.New("mode must be plan, rotate, or verify")
	}
	if err != nil {
		exitWithError(err)
	}
	encoded, err := common.Marshal(report)
	if err != nil {
		exitWithError(err)
	}
	fmt.Println(string(encoded))
}

func openDatabase(dsn string) (*gorm.DB, error) {
	dsn = strings.TrimSpace(dsn)
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	case dsn == "", strings.HasPrefix(dsn, "local"):
		return gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{})
	default:
		if !strings.Contains(dsn, "parseTime") {
			if strings.Contains(dsn, "?") {
				dsn += "&parseTime=true"
			} else {
				dsn += "?parseTime=true"
			}
		}
		return gorm.Open(mysql.Open(dsn), &gorm.Config{})
	}
}

func exitWithError(err error) {
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}
