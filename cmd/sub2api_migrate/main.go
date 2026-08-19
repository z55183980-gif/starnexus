package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const sourceSystemSub2API = "sub2api"

type options struct {
	Mode              string
	SourceDSN         string
	DestinationDSN    string
	RunID             string
	AutoMigrateSchema bool
}

type sourceAccount struct {
	ID                      int64      `gorm:"column:id"`
	Name                    string     `gorm:"column:name"`
	Notes                   *string    `gorm:"column:notes"`
	Platform                string     `gorm:"column:platform"`
	Type                    string     `gorm:"column:type"`
	Credentials             []byte     `gorm:"column:credentials"`
	Extra                   []byte     `gorm:"column:extra"`
	ProxyID                 *int64     `gorm:"column:proxy_id"`
	ProxyFallbackOriginID   *int64     `gorm:"column:proxy_fallback_origin_id"`
	Concurrency             int        `gorm:"column:concurrency"`
	LoadFactor              *int       `gorm:"column:load_factor"`
	Priority                int        `gorm:"column:priority"`
	RateMultiplier          float64    `gorm:"column:rate_multiplier"`
	Status                  string     `gorm:"column:status"`
	ErrorMessage            *string    `gorm:"column:error_message"`
	LastUsedAt              *time.Time `gorm:"column:last_used_at"`
	ExpiresAt               *time.Time `gorm:"column:expires_at"`
	AutoPauseOnExpired      bool       `gorm:"column:auto_pause_on_expired"`
	Schedulable             bool       `gorm:"column:schedulable"`
	RateLimitedAt           *time.Time `gorm:"column:rate_limited_at"`
	RateLimitResetAt        *time.Time `gorm:"column:rate_limit_reset_at"`
	OverloadUntil           *time.Time `gorm:"column:overload_until"`
	TempUnschedulableUntil  *time.Time `gorm:"column:temp_unschedulable_until"`
	TempUnschedulableReason *string    `gorm:"column:temp_unschedulable_reason"`
	SessionWindowStart      *time.Time `gorm:"column:session_window_start"`
	SessionWindowEnd        *time.Time `gorm:"column:session_window_end"`
	SessionWindowStatus     *string    `gorm:"column:session_window_status"`
	DeletedAt               *time.Time `gorm:"column:deleted_at"`
}

func (sourceAccount) TableName() string { return "accounts" }

type sourceProxy struct {
	ID             int64      `gorm:"column:id"`
	Name           string     `gorm:"column:name"`
	Protocol       string     `gorm:"column:protocol"`
	Host           string     `gorm:"column:host"`
	Port           int        `gorm:"column:port"`
	Username       *string    `gorm:"column:username"`
	Password       *string    `gorm:"column:password"`
	Status         string     `gorm:"column:status"`
	ExpiresAt      *time.Time `gorm:"column:expires_at"`
	FallbackMode   string     `gorm:"column:fallback_mode"`
	BackupProxyID  *int64     `gorm:"column:backup_proxy_id"`
	ExpiryWarnDays int        `gorm:"column:expiry_warn_days"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
}

func (sourceProxy) TableName() string { return "proxies" }

type sourceGroup struct {
	ID                  int64      `gorm:"column:id"`
	Name                string     `gorm:"column:name"`
	Description         *string    `gorm:"column:description"`
	Status              string     `gorm:"column:status"`
	Platform            string     `gorm:"column:platform"`
	ModelRouting        []byte     `gorm:"column:model_routing"`
	ModelRoutingEnabled bool       `gorm:"column:model_routing_enabled"`
	DefaultMappedModel  string     `gorm:"column:default_mapped_model"`
	DeletedAt           *time.Time `gorm:"column:deleted_at"`
}

func (sourceGroup) TableName() string { return "groups" }

type sourceAccountGroup struct {
	AccountID int64 `gorm:"column:account_id"`
	GroupID   int64 `gorm:"column:group_id"`
	Priority  int   `gorm:"column:priority"`
}

func (sourceAccountGroup) TableName() string { return "account_groups" }

type sourceSnapshot struct {
	Accounts     []sourceAccount
	Proxies      []sourceProxy
	Groups       []sourceGroup
	Memberships  []sourceAccountGroup
	AccountTypes map[int64]string
}

type migrationReport struct {
	Mode                string `json:"mode"`
	RunID               string `json:"run_id"`
	SourceAccounts      int    `json:"source_accounts"`
	SourcePools         int    `json:"source_pools"`
	SourceProxies       int    `json:"source_proxies"`
	SourceMemberships   int    `json:"source_memberships"`
	CreatedAccounts     int    `json:"created_accounts"`
	UpdatedAccounts     int    `json:"updated_accounts"`
	CreatedPools        int    `json:"created_pools"`
	UpdatedPools        int    `json:"updated_pools"`
	CreatedProxies      int    `json:"created_proxies"`
	UpdatedProxies      int    `json:"updated_proxies"`
	ReplacedMemberships int    `json:"replaced_memberships"`
	VerifiedAccounts    int    `json:"verified_accounts"`
	VerifiedPools       int    `json:"verified_pools"`
	VerifiedProxies     int    `json:"verified_proxies"`
	RemovedAccounts     int64  `json:"removed_accounts"`
	RemovedPools        int64  `json:"removed_pools"`
	RemovedProxies      int64  `json:"removed_proxies"`
}

// destinationChannelAccountPoolColumns limits the migration tool to the two
// channel columns it owns, without asking GORM to reconcile the full table.
type destinationChannelAccountPoolColumns struct {
	Id                    int    `gorm:"column:id;primaryKey"`
	CredentialSource      string `gorm:"column:credential_source;type:varchar(32);not null;default:'channel_key';index"`
	UpstreamAccountPoolId *int   `gorm:"column:upstream_account_pool_id;index"`
}

func (destinationChannelAccountPoolColumns) TableName() string { return "channels" }

func main() {
	var opts options
	flag.StringVar(&opts.Mode, "mode", "plan", "plan, import, verify, or rollback")
	flag.StringVar(&opts.SourceDSN, "source-dsn", os.Getenv("SUB2API_SQL_DSN"), "sub2api PostgreSQL DSN")
	flag.StringVar(&opts.DestinationDSN, "destination-dsn", os.Getenv("SQL_DSN"), "StarNexus database DSN")
	flag.StringVar(&opts.RunID, "run-id", "sub2api-v0.1.165", "stable idempotent import identifier")
	flag.BoolVar(&opts.AutoMigrateSchema, "auto-migrate-schema", false, "create the StarNexus upstream-account schema before importing")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "sub2api migration failed:", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	opts.Mode = strings.ToLower(strings.TrimSpace(opts.Mode))
	opts.RunID = strings.TrimSpace(opts.RunID)
	if opts.RunID == "" {
		return errors.New("run-id is required")
	}
	if opts.DestinationDSN == "" {
		return errors.New("destination-dsn or SQL_DSN is required")
	}
	destination, err := openDatabase(opts.DestinationDSN)
	if err != nil {
		return fmt.Errorf("open destination database: %w", err)
	}
	model.DB = destination
	if opts.AutoMigrateSchema {
		if err := migrateDestinationSchema(destination); err != nil {
			return fmt.Errorf("migrate destination schema: %w", err)
		}
	}

	if opts.Mode == "rollback" {
		report, err := rollback(destination, opts.RunID)
		if err != nil {
			return err
		}
		return printReport(report)
	}
	if opts.SourceDSN == "" {
		return errors.New("source-dsn or SUB2API_SQL_DSN is required")
	}
	source, err := openDatabase(opts.SourceDSN)
	if err != nil {
		return fmt.Errorf("open source database: %w", err)
	}
	snapshot, err := loadSourceSnapshot(source)
	if err != nil {
		return err
	}

	switch opts.Mode {
	case "plan":
		return printReport(reportForSnapshot("plan", opts.RunID, snapshot))
	case "import":
		report, err := importSnapshot(destination, snapshot, opts.RunID)
		if err != nil {
			return err
		}
		return printReport(report)
	case "verify":
		report, err := verifySnapshot(destination, snapshot, opts.RunID)
		if err != nil {
			return err
		}
		return printReport(report)
	default:
		return errors.New("mode must be plan, import, verify, or rollback")
	}
}

func migrateDestinationSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.Channel{}) {
		return errors.New("destination channels table is missing")
	}
	return db.AutoMigrate(
		&destinationChannelAccountPoolColumns{},
		&model.UpstreamProxy{}, &model.UpstreamAccountPool{}, &model.UpstreamAccount{},
		&model.UpstreamAccountPoolMember{}, &model.UpstreamAccountEvent{}, &model.UpstreamOAuthSession{},
	)
}

func openDatabase(dsn string) (*gorm.DB, error) {
	dsn = strings.TrimSpace(dsn)
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	case strings.HasPrefix(dsn, "sqlite://"):
		return gorm.Open(sqlite.Open(strings.TrimPrefix(dsn, "sqlite://")), &gorm.Config{})
	default:
		if !strings.Contains(dsn, "parseTime=") {
			separator := "?"
			if strings.Contains(dsn, "?") {
				separator = "&"
			}
			dsn += separator + "parseTime=true"
		}
		return gorm.Open(mysql.Open(dsn), &gorm.Config{})
	}
}

func loadSourceSnapshot(db *gorm.DB) (*sourceSnapshot, error) {
	var snapshot sourceSnapshot
	if err := db.Where("platform = ? AND LOWER(type) IN ? AND deleted_at IS NULL", "openai", []string{"oauth", "api_key", "apikey"}).Order("id ASC").Find(&snapshot.Accounts).Error; err != nil {
		return nil, fmt.Errorf("load source accounts: %w", err)
	}
	proxies, err := loadReferencedSourceProxies(db, snapshot.Accounts)
	if err != nil {
		return nil, err
	}
	snapshot.Proxies = proxies
	accountIDs := make([]int64, 0, len(snapshot.Accounts))
	snapshot.AccountTypes = make(map[int64]string, len(snapshot.Accounts))
	for _, account := range snapshot.Accounts {
		accountIDs = append(accountIDs, account.ID)
		snapshot.AccountTypes[account.ID] = mapAccountType(account.Type)
	}
	if len(accountIDs) > 0 {
		var memberships []sourceAccountGroup
		if err := db.Where("account_id IN ?", accountIDs).Order("group_id ASC").Order("priority ASC").Find(&memberships).Error; err != nil {
			return nil, fmt.Errorf("load source memberships: %w", err)
		}
		groupIDs := make([]int64, 0, len(memberships))
		seenGroupIDs := make(map[int64]struct{}, len(memberships))
		for _, membership := range memberships {
			if _, exists := seenGroupIDs[membership.GroupID]; !exists {
				seenGroupIDs[membership.GroupID] = struct{}{}
				groupIDs = append(groupIDs, membership.GroupID)
			}
		}
		if len(groupIDs) > 0 {
			if err := db.Where("id IN ? AND platform = ? AND deleted_at IS NULL", groupIDs, "openai").Order("id ASC").Find(&snapshot.Groups).Error; err != nil {
				return nil, fmt.Errorf("load source groups: %w", err)
			}
			allowedGroups := make(map[int64]struct{}, len(snapshot.Groups))
			for _, group := range snapshot.Groups {
				allowedGroups[group.ID] = struct{}{}
			}
			for _, membership := range memberships {
				if _, allowed := allowedGroups[membership.GroupID]; allowed {
					snapshot.Memberships = append(snapshot.Memberships, membership)
				}
			}
		}
	}
	return &snapshot, nil
}

func loadReferencedSourceProxies(db *gorm.DB, accounts []sourceAccount) ([]sourceProxy, error) {
	pending := make(map[int64]struct{})
	for _, account := range accounts {
		if account.ProxyID != nil {
			pending[*account.ProxyID] = struct{}{}
		}
		if account.ProxyFallbackOriginID != nil {
			pending[*account.ProxyFallbackOriginID] = struct{}{}
		}
	}
	loaded := make(map[int64]sourceProxy)
	for len(pending) > 0 {
		ids := make([]int64, 0, len(pending))
		for id := range pending {
			if _, exists := loaded[id]; !exists {
				ids = append(ids, id)
			}
		}
		pending = make(map[int64]struct{})
		if len(ids) == 0 {
			break
		}
		var batch []sourceProxy
		if err := db.Where("id IN ? AND deleted_at IS NULL", ids).Find(&batch).Error; err != nil {
			return nil, fmt.Errorf("load referenced source proxies: %w", err)
		}
		for _, proxy := range batch {
			if err := validateSourceProxy(proxy); err != nil {
				return nil, fmt.Errorf("source proxy %d is invalid: %w", proxy.ID, err)
			}
			loaded[proxy.ID] = proxy
			if proxy.BackupProxyID != nil {
				if _, exists := loaded[*proxy.BackupProxyID]; !exists {
					pending[*proxy.BackupProxyID] = struct{}{}
				}
			}
		}
		for _, id := range ids {
			if _, exists := loaded[id]; !exists {
				return nil, fmt.Errorf("referenced source proxy %d is missing or deleted", id)
			}
		}
	}
	proxies := make([]sourceProxy, 0, len(loaded))
	for _, proxy := range loaded {
		proxies = append(proxies, proxy)
	}
	sort.Slice(proxies, func(i, j int) bool { return proxies[i].ID < proxies[j].ID })
	return proxies, nil
}

func reportForSnapshot(mode string, runID string, snapshot *sourceSnapshot) migrationReport {
	return migrationReport{
		Mode: mode, RunID: runID, SourceAccounts: len(snapshot.Accounts), SourcePools: len(snapshot.Groups),
		SourceProxies: len(snapshot.Proxies), SourceMemberships: len(snapshot.Memberships),
	}
}

func importSnapshot(db *gorm.DB, snapshot *sourceSnapshot, runID string) (migrationReport, error) {
	report := reportForSnapshot("import", runID, snapshot)
	proxyMap := make(map[int64]int, len(snapshot.Proxies))
	for _, source := range snapshot.Proxies {
		proxy, created, err := upsertProxy(db, source, runID, nil)
		if err != nil {
			return report, fmt.Errorf("import proxy %d: %w", source.ID, err)
		}
		proxyMap[source.ID] = proxy.Id
		if created {
			report.CreatedProxies++
		} else {
			report.UpdatedProxies++
		}
	}
	for _, source := range snapshot.Proxies {
		if source.BackupProxyID == nil {
			continue
		}
		proxyID := proxyMap[source.ID]
		backupID, ok := proxyMap[*source.BackupProxyID]
		if !ok {
			return report, fmt.Errorf("proxy %d references unavailable backup proxy %d", source.ID, *source.BackupProxyID)
		}
		var current model.UpstreamProxy
		if err := db.First(&current, proxyID).Error; err != nil {
			return report, err
		}
		current.BackupProxyId = &backupID
		current.FallbackMode = normalizeFallbackMode(source.FallbackMode, true)
		if err := service.UpdateUpstreamProxy(&service.UpstreamProxyUpdateInput{Proxy: current}); err != nil {
			return report, fmt.Errorf("link proxy fallback %d: %w", source.ID, err)
		}
	}

	poolMap := make(map[int64]int, len(snapshot.Groups))
	for _, source := range snapshot.Groups {
		pool, created, err := upsertPool(db, source, poolCredentialType(source.ID, snapshot), runID)
		if err != nil {
			return report, fmt.Errorf("import group %d: %w", source.ID, err)
		}
		poolMap[source.ID] = pool.Id
		if created {
			report.CreatedPools++
		} else {
			report.UpdatedPools++
		}
	}

	accountMap := make(map[int64]int, len(snapshot.Accounts))
	for _, source := range snapshot.Accounts {
		account, created, err := upsertAccount(db, source, proxyMap, runID)
		if err != nil {
			return report, fmt.Errorf("import account %d: %w", source.ID, err)
		}
		accountMap[source.ID] = account.Id
		if created {
			report.CreatedAccounts++
		} else {
			report.UpdatedAccounts++
		}
	}
	for _, source := range snapshot.Groups {
		destinationPoolId, ok := poolMap[source.ID]
		if !ok {
			continue
		}
		if err := db.Model(&model.UpstreamAccountPool{}).Where("id = ?", destinationPoolId).
			Update("scheduler_config", schedulerConfig(source, accountMap)).Error; err != nil {
			return report, fmt.Errorf("remap group %d model routing: %w", source.ID, err)
		}
	}

	membersByPool := make(map[int64][]service.UpstreamAccountPoolMemberInput)
	for _, source := range snapshot.Memberships {
		accountID, accountOK := accountMap[source.AccountID]
		_, poolOK := poolMap[source.GroupID]
		if !accountOK || !poolOK {
			continue
		}
		membersByPool[source.GroupID] = append(membersByPool[source.GroupID], service.UpstreamAccountPoolMemberInput{
			AccountId: accountID,
		})
	}
	for sourcePoolID, destinationPoolID := range poolMap {
		members := membersByPool[sourcePoolID]
		if err := reconcileImportedPoolMembers(db, destinationPoolID, members); err != nil {
			return report, fmt.Errorf("reconcile pool %d memberships: %w", sourcePoolID, err)
		}
		report.ReplacedMemberships += len(members)
	}
	return report, nil
}

func reconcileImportedPoolMembers(db *gorm.DB, poolID int, desired []service.UpstreamAccountPoolMemberInput) error {
	if poolID <= 0 {
		return errors.New("destination pool id is required")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var importedAccountIDs []int
		if err := tx.Table("upstream_account_pool_members").
			Select("upstream_account_pool_members.account_id").
			Joins("JOIN upstream_accounts ON upstream_accounts.id = upstream_account_pool_members.account_id").
			Where("upstream_account_pool_members.pool_id = ? AND upstream_accounts.source_system = ?", poolID, sourceSystemSub2API).
			Pluck("upstream_account_pool_members.account_id", &importedAccountIDs).Error; err != nil {
			return err
		}

		desiredIDs := make(map[int]struct{}, len(desired))
		for _, input := range desired {
			if input.AccountId <= 0 {
				return fmt.Errorf("invalid imported membership for account %d", input.AccountId)
			}
			desiredIDs[input.AccountId] = struct{}{}
		}
		var accounts []model.UpstreamAccount
		if len(desiredIDs) > 0 {
			accountIDs := make([]int, 0, len(desiredIDs))
			for accountID := range desiredIDs {
				accountIDs = append(accountIDs, accountID)
			}
			if err := tx.Where("id IN ?", accountIDs).Find(&accounts).Error; err != nil {
				return err
			}
			if len(accounts) != len(desiredIDs) {
				return errors.New("an imported pool membership references an invalid account")
			}
		}
		accountsByID := make(map[int]model.UpstreamAccount, len(accounts))
		for _, account := range accounts {
			accountsByID[account.Id] = account
		}
		removeIDs := make([]int, 0)
		for _, accountID := range importedAccountIDs {
			if _, keep := desiredIDs[accountID]; !keep {
				removeIDs = append(removeIDs, accountID)
			}
		}
		if len(removeIDs) > 0 {
			if err := tx.Where("pool_id = ? AND account_id IN ?", poolID, removeIDs).
				Delete(&model.UpstreamAccountPoolMember{}).Error; err != nil {
				return err
			}
		}

		now := common.GetTimestamp()
		for _, input := range desired {
			account := accountsByID[input.AccountId]
			weight := max(1, account.Weight)
			member := model.UpstreamAccountPoolMember{
				PoolId: poolID, AccountId: input.AccountId, Priority: account.Priority,
				Weight: weight, CreatedAt: now,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "pool_id"}, {Name: "account_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"priority", "weight"}),
			}).Create(&member).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func upsertProxy(db *gorm.DB, source sourceProxy, runID string, backupID *int) (*model.UpstreamProxy, bool, error) {
	var current model.UpstreamProxy
	err := db.Where("source_system = ? AND source_id = ?", sourceSystemSub2API, source.ID).First(&current).Error
	created := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !created {
		return nil, false, err
	}
	proxy := current
	if created {
		proxy.SourceSystem = stringPointer(sourceSystemSub2API)
		proxy.SourceId = int64Pointer(source.ID)
		proxy.ImportRunId = stringPointer(runID)
	}
	proxy.Name = source.Name
	proxy.Protocol = normalizeProxyProtocol(source.Protocol)
	proxy.Host = source.Host
	proxy.Port = source.Port
	proxy.Status = mapActiveStatus(source.Status)
	proxy.ExpiresAt = unixPointer(source.ExpiresAt)
	proxy.FallbackMode = normalizeFallbackMode(source.FallbackMode, backupID != nil)
	proxy.BackupProxyId = backupID
	proxy.ExpiryWarnDays = source.ExpiryWarnDays
	auth := service.UpstreamProxyAuthInput{Username: stringValue(source.Username), Password: stringValue(source.Password)}
	if created {
		input := &service.UpstreamProxyCreateInput{Proxy: proxy, Auth: auth}
		if err := service.CreateUpstreamProxy(input); err != nil {
			return nil, false, err
		}
		proxy = input.Proxy
		return &proxy, true, nil
	}
	credentials := &auth
	if existing, decryptErr := service.DecryptUpstreamProxyAuth(&current); decryptErr == nil && reflect.DeepEqual(*existing, auth) {
		credentials = nil
	}
	if err := service.UpdateUpstreamProxy(&service.UpstreamProxyUpdateInput{Proxy: proxy, Auth: credentials}); err != nil {
		return nil, false, err
	}
	return &proxy, false, nil
}

func upsertPool(db *gorm.DB, source sourceGroup, credentialType string, runID string) (*model.UpstreamAccountPool, bool, error) {
	var current model.UpstreamAccountPool
	err := db.Where("source_system = ? AND source_id = ?", sourceSystemSub2API, source.ID).First(&current).Error
	created := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !created {
		return nil, false, err
	}
	pool := current
	if created {
		pool.SourceSystem = stringPointer(sourceSystemSub2API)
		pool.SourceId = int64Pointer(source.ID)
		pool.ImportRunId = stringPointer(runID)
	}
	pool.Name = uniqueImportedPoolName(db, source.Name, source.ID, pool.Id)
	pool.Description = stringValue(source.Description)
	pool.Platform = constant.UpstreamPlatformOpenAI
	pool.CredentialType = credentialType
	pool.Status = mapActiveStatus(source.Status)
	pool.SchedulerConfig = schedulerConfig(source, nil)
	if created {
		if err := service.CreateUpstreamAccountPool(&pool); err != nil {
			return nil, false, err
		}
		return &pool, true, nil
	}
	if err := service.UpdateUpstreamAccountPool(&pool); err != nil {
		return nil, false, err
	}
	return &pool, false, nil
}

func upsertAccount(db *gorm.DB, source sourceAccount, proxyMap map[int64]int, runID string) (*model.UpstreamAccount, bool, error) {
	var credentials map[string]any
	if err := common.Unmarshal(source.Credentials, &credentials); err != nil {
		return nil, false, errors.New("source credentials are invalid JSON")
	}
	accountType := mapAccountType(source.Type)
	if accountType == "" {
		return nil, false, fmt.Errorf("unsupported account type %q", source.Type)
	}
	credentials = normalizeSourceAccountCredentials(accountType, credentials)
	var current model.UpstreamAccount
	err := db.Where("source_system = ? AND source_id = ?", sourceSystemSub2API, source.ID).First(&current).Error
	created := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !created {
		return nil, false, err
	}
	account := current
	if created {
		account.SourceSystem = stringPointer(sourceSystemSub2API)
		account.SourceId = int64Pointer(source.ID)
		account.ImportRunId = stringPointer(runID)
	}
	account.Name = source.Name
	account.Notes = source.Notes
	account.Platform = constant.UpstreamPlatformOpenAI
	account.Type = accountType
	account.Extra = jsonText(source.Extra)
	account.ProxyId = mappedIntPointer(source.ProxyID, proxyMap)
	account.ProxyFallbackOriginId = mappedIntPointer(source.ProxyFallbackOriginID, proxyMap)
	account.Concurrency = max(1, source.Concurrency)
	account.LoadFactor = source.LoadFactor
	account.Priority = source.Priority
	account.Weight = 1
	account.RateMultiplier = float64Pointer(source.RateMultiplier)
	account.Status = mapAccountStatus(source.Status)
	account.Schedulable = source.Schedulable
	account.ErrorMessage = stringValue(source.ErrorMessage)
	account.LastUsedAt = unixPointer(source.LastUsedAt)
	account.ExpiresAt = sourceAccountExpiresAt(source, accountType, credentials)
	account.AutoPauseOnExpired = source.AutoPauseOnExpired
	account.OAuthRefreshOwner = constant.UpstreamOAuthRefreshOwnerExternal
	account.RateLimitedAt = unixPointer(source.RateLimitedAt)
	account.RateLimitResetAt = unixPointer(source.RateLimitResetAt)
	account.OverloadUntil = unixPointer(source.OverloadUntil)
	account.TempUnschedulableUntil = unixPointer(source.TempUnschedulableUntil)
	account.TempUnschedulableReason = stringValue(source.TempUnschedulableReason)
	account.SessionWindowStart = unixPointer(source.SessionWindowStart)
	account.SessionWindowEnd = unixPointer(source.SessionWindowEnd)
	account.SessionWindowStatus = stringValue(source.SessionWindowStatus)
	if created {
		input := &service.UpstreamAccountCreateInput{Account: account, Credentials: credentials}
		if err := service.CreateUpstreamAccount(input); err != nil {
			return nil, false, err
		}
		account = input.Account
		return &account, true, nil
	}
	credentialUpdate := &credentials
	if existing, decryptErr := service.DecryptUpstreamAccountCredentials(&current); decryptErr == nil && reflect.DeepEqual(existing, credentials) {
		credentialUpdate = nil
	}
	if err := service.UpdateUpstreamAccount(&service.UpstreamAccountUpdateInput{Account: account, Credentials: credentialUpdate}); err != nil {
		return nil, false, err
	}
	return &account, false, nil
}

func verifySnapshot(db *gorm.DB, snapshot *sourceSnapshot, runID string) (migrationReport, error) {
	report := reportForSnapshot("verify", runID, snapshot)
	accountMap := make(map[int64]model.UpstreamAccount, len(snapshot.Accounts))
	for _, source := range snapshot.Accounts {
		if err := verifySingleSourceRecord(db, &model.UpstreamAccount{}, source.ID); err != nil {
			return report, fmt.Errorf("verify account %d: %w", source.ID, err)
		}
		var account model.UpstreamAccount
		if err := db.Where("source_system = ? AND source_id = ?", sourceSystemSub2API, source.ID).First(&account).Error; err != nil {
			return report, err
		}
		if err := verifyImportedAccount(db, source, &account); err != nil {
			return report, fmt.Errorf("verify account %d: %w", source.ID, err)
		}
		accountMap[source.ID] = account
		report.VerifiedAccounts++
	}
	poolMap := make(map[int64]model.UpstreamAccountPool, len(snapshot.Groups))
	accountIdMap := make(map[int64]int, len(accountMap))
	for sourceId, account := range accountMap {
		accountIdMap[sourceId] = account.Id
	}
	for _, source := range snapshot.Groups {
		if err := verifySingleSourceRecord(db, &model.UpstreamAccountPool{}, source.ID); err != nil {
			return report, fmt.Errorf("verify group %d: %w", source.ID, err)
		}
		var pool model.UpstreamAccountPool
		if err := db.Where("source_system = ? AND source_id = ?", sourceSystemSub2API, source.ID).First(&pool).Error; err != nil {
			return report, err
		}
		if pool.Platform != constant.UpstreamPlatformOpenAI || pool.CredentialType != poolCredentialType(source.ID, snapshot) ||
			pool.Status != mapActiveStatus(source.Status) || pool.SchedulerConfig != schedulerConfig(source, accountIdMap) {
			return report, fmt.Errorf("group %d destination fields do not match source", source.ID)
		}
		poolMap[source.ID] = pool
		report.VerifiedPools++
	}
	for _, source := range snapshot.Proxies {
		if err := verifySingleSourceRecord(db, &model.UpstreamProxy{}, source.ID); err != nil {
			return report, fmt.Errorf("verify proxy %d: %w", source.ID, err)
		}
		var proxy model.UpstreamProxy
		if err := db.Where("source_system = ? AND source_id = ?", sourceSystemSub2API, source.ID).First(&proxy).Error; err != nil {
			return report, err
		}
		if err := verifyImportedProxy(db, source, &proxy); err != nil {
			return report, fmt.Errorf("verify proxy %d: %w", source.ID, err)
		}
		report.VerifiedProxies++
	}
	if err := verifyImportedMemberships(db, snapshot.Memberships, accountMap, poolMap); err != nil {
		return report, err
	}
	return report, nil
}

func verifySingleSourceRecord(db *gorm.DB, destination any, sourceID int64) error {
	var count int64
	if err := db.Model(destination).Where("source_system = ? AND source_id = ?", sourceSystemSub2API, sourceID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("source record maps to %d destination records", count)
	}
	return nil
}

func verifyImportedAccount(db *gorm.DB, source sourceAccount, account *model.UpstreamAccount) error {
	if account.Platform != constant.UpstreamPlatformOpenAI || account.Type != mapAccountType(source.Type) ||
		account.Concurrency != max(1, source.Concurrency) || account.Priority != source.Priority ||
		account.Status != mapAccountStatus(source.Status) || account.Schedulable != source.Schedulable ||
		account.AutoPauseOnExpired != source.AutoPauseOnExpired ||
		account.OAuthRefreshOwner != constant.UpstreamOAuthRefreshOwnerExternal {
		return errors.New("destination fields do not match source")
	}
	credentials, err := service.DecryptUpstreamAccountCredentials(account)
	if err != nil {
		return fmt.Errorf("decrypt credentials: %w", err)
	}
	var expectedCredentials map[string]any
	if err := common.Unmarshal(source.Credentials, &expectedCredentials); err != nil {
		return errors.New("source credentials are invalid JSON")
	}
	expectedCredentials = normalizeSourceAccountCredentials(mapAccountType(source.Type), expectedCredentials)
	if !reflect.DeepEqual(credentials, expectedCredentials) {
		return errors.New("credential mismatch")
	}
	if !int64PointersEqual(account.ExpiresAt, sourceAccountExpiresAt(source, mapAccountType(source.Type), expectedCredentials)) {
		return errors.New("account expiry mismatch")
	}
	if err := verifyImportedProxyReference(db, account.ProxyId, source.ProxyID); err != nil {
		return fmt.Errorf("proxy reference: %w", err)
	}
	if err := verifyImportedProxyReference(db, account.ProxyFallbackOriginId, source.ProxyFallbackOriginID); err != nil {
		return fmt.Errorf("fallback proxy reference: %w", err)
	}
	return nil
}

func verifyImportedProxy(db *gorm.DB, source sourceProxy, proxy *model.UpstreamProxy) error {
	expectedFallback := normalizeFallbackMode(source.FallbackMode, source.BackupProxyID != nil)
	if proxy.Protocol != normalizeProxyProtocol(source.Protocol) || proxy.Host != source.Host || proxy.Port != source.Port ||
		proxy.Status != mapActiveStatus(source.Status) || proxy.FallbackMode != expectedFallback ||
		proxy.ExpiryWarnDays != source.ExpiryWarnDays {
		return errors.New("destination fields do not match source")
	}
	auth, err := service.DecryptUpstreamProxyAuth(proxy)
	if err != nil {
		return fmt.Errorf("decrypt authentication: %w", err)
	}
	expectedAuth := service.UpstreamProxyAuthInput{Username: stringValue(source.Username), Password: stringValue(source.Password)}
	if !reflect.DeepEqual(*auth, expectedAuth) {
		return errors.New("authentication mismatch")
	}
	return verifyImportedProxyReference(db, proxy.BackupProxyId, source.BackupProxyID)
}

func verifyImportedProxyReference(db *gorm.DB, destinationID *int, sourceID *int64) error {
	if sourceID == nil {
		if destinationID != nil {
			return errors.New("unexpected destination proxy reference")
		}
		return nil
	}
	if destinationID == nil {
		return fmt.Errorf("missing destination reference for source proxy %d", *sourceID)
	}
	var proxy model.UpstreamProxy
	if err := db.Select("id", "source_system", "source_id").First(&proxy, *destinationID).Error; err != nil {
		return err
	}
	if proxy.SourceSystem == nil || *proxy.SourceSystem != sourceSystemSub2API || proxy.SourceId == nil || *proxy.SourceId != *sourceID {
		return fmt.Errorf("destination proxy %d does not map to source proxy %d", *destinationID, *sourceID)
	}
	return nil
}

func verifyImportedMemberships(db *gorm.DB, sources []sourceAccountGroup, accounts map[int64]model.UpstreamAccount, pools map[int64]model.UpstreamAccountPool) error {
	expected := make(map[string]sourceAccountGroup, len(sources))
	for _, source := range sources {
		expected[fmt.Sprintf("%d:%d", source.GroupID, source.AccountID)] = source
	}
	for key, source := range expected {
		account, accountOK := accounts[source.AccountID]
		pool, poolOK := pools[source.GroupID]
		if !accountOK || !poolOK {
			return fmt.Errorf("membership %s references an unavailable imported record", key)
		}
		var member model.UpstreamAccountPoolMember
		if err := db.Where("pool_id = ? AND account_id = ?", pool.Id, account.Id).First(&member).Error; err != nil {
			return fmt.Errorf("membership %s: %w", key, err)
		}
		if member.Priority != source.Priority || member.Weight != 1 {
			return fmt.Errorf("membership %s fields do not match source", key)
		}
	}

	var importedMemberships int64
	if err := db.Model(&model.UpstreamAccountPoolMember{}).
		Joins("JOIN upstream_accounts ON upstream_accounts.id = upstream_account_pool_members.account_id").
		Joins("JOIN upstream_account_pools ON upstream_account_pools.id = upstream_account_pool_members.pool_id").
		Where("upstream_accounts.source_system = ? AND upstream_account_pools.source_system = ?", sourceSystemSub2API, sourceSystemSub2API).
		Count(&importedMemberships).Error; err != nil {
		return err
	}
	if importedMemberships != int64(len(expected)) {
		return fmt.Errorf("membership count mismatch: source=%d destination=%d", len(expected), importedMemberships)
	}
	return nil
}

func rollback(db *gorm.DB, runID string) (migrationReport, error) {
	report := migrationReport{Mode: "rollback", RunID: runID}
	err := db.Transaction(func(tx *gorm.DB) error {
		var poolIDs []int
		var accountIDs []int
		var proxyIDs []int
		if err := tx.Model(&model.UpstreamAccountPool{}).Where("source_system = ? AND import_run_id = ?", sourceSystemSub2API, runID).Pluck("id", &poolIDs).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.UpstreamAccount{}).Where("source_system = ? AND import_run_id = ?", sourceSystemSub2API, runID).Pluck("id", &accountIDs).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.UpstreamProxy{}).Where("source_system = ? AND import_run_id = ?", sourceSystemSub2API, runID).Pluck("id", &proxyIDs).Error; err != nil {
			return err
		}
		if len(poolIDs) > 0 {
			var channels int64
			if err := tx.Model(&model.Channel{}).Where("upstream_account_pool_id IN ?", poolIDs).Count(&channels).Error; err != nil {
				return err
			}
			if channels > 0 {
				return fmt.Errorf("rollback blocked: %d channel(s) reference imported pools", channels)
			}
			var externalMemberships int64
			membershipQuery := tx.Model(&model.UpstreamAccountPoolMember{}).Where("pool_id IN ?", poolIDs)
			if len(accountIDs) > 0 {
				membershipQuery = membershipQuery.Where("account_id NOT IN ?", accountIDs)
			}
			if err := membershipQuery.Count(&externalMemberships).Error; err != nil {
				return err
			}
			if externalMemberships > 0 {
				return fmt.Errorf("rollback blocked: %d external account membership(s) reference imported pools", externalMemberships)
			}
		}
		if len(accountIDs) > 0 {
			var externalMemberships int64
			membershipQuery := tx.Model(&model.UpstreamAccountPoolMember{}).Where("account_id IN ?", accountIDs)
			if len(poolIDs) > 0 {
				membershipQuery = membershipQuery.Where("pool_id NOT IN ?", poolIDs)
			}
			if err := membershipQuery.Count(&externalMemberships).Error; err != nil {
				return err
			}
			if externalMemberships > 0 {
				return fmt.Errorf("rollback blocked: %d external pool membership(s) reference imported accounts", externalMemberships)
			}
		}
		if len(proxyIDs) > 0 {
			externalSource := "(source_system IS NULL OR source_system <> ? OR import_run_id IS NULL OR import_run_id <> ?)"
			checks := []struct {
				model        any
				query        string
				label        string
				twoProxyArgs bool
			}{
				{&model.UpstreamAccount{}, "(proxy_id IN ? OR proxy_fallback_origin_id IN ?) AND " + externalSource, "external account", true},
				{&model.UpstreamAccountPool{}, "default_proxy_id IN ? AND " + externalSource, "external account pool", false},
				{&model.UpstreamProxy{}, "backup_proxy_id IN ? AND " + externalSource, "external proxy fallback", false},
			}
			for _, check := range checks {
				var count int64
				query := tx.Model(check.model)
				if check.twoProxyArgs {
					query = query.Where(check.query, proxyIDs, proxyIDs, sourceSystemSub2API, runID)
				} else {
					query = query.Where(check.query, proxyIDs, sourceSystemSub2API, runID)
				}
				if err := query.Count(&count).Error; err != nil {
					return err
				}
				if count > 0 {
					return fmt.Errorf("rollback blocked: %d %s record(s) reference imported proxies", count, check.label)
				}
			}
		}
		if len(accountIDs) > 0 {
			if err := tx.Where("account_id IN ?", accountIDs).Delete(&model.UpstreamAccountPoolMember{}).Error; err != nil {
				return err
			}
			result := tx.Where("id IN ?", accountIDs).Delete(&model.UpstreamAccount{})
			if result.Error != nil {
				return result.Error
			}
			report.RemovedAccounts = result.RowsAffected
		}
		if len(poolIDs) > 0 {
			if err := tx.Where("pool_id IN ?", poolIDs).Delete(&model.UpstreamAccountPoolMember{}).Error; err != nil {
				return err
			}
			result := tx.Where("id IN ?", poolIDs).Delete(&model.UpstreamAccountPool{})
			if result.Error != nil {
				return result.Error
			}
			report.RemovedPools = result.RowsAffected
		}
		if len(proxyIDs) > 0 {
			if err := tx.Model(&model.UpstreamProxy{}).Where("id IN ?", proxyIDs).Update("backup_proxy_id", nil).Error; err != nil {
				return err
			}
			result := tx.Where("id IN ?", proxyIDs).Delete(&model.UpstreamProxy{})
			if result.Error != nil {
				return result.Error
			}
			report.RemovedProxies = result.RowsAffected
		}
		return nil
	})
	return report, err
}

func poolCredentialType(groupID int64, snapshot *sourceSnapshot) string {
	types := map[string]struct{}{}
	for _, membership := range snapshot.Memberships {
		if membership.GroupID == groupID {
			if accountType := snapshot.AccountTypes[membership.AccountID]; accountType != "" {
				types[accountType] = struct{}{}
			}
		}
	}
	if len(types) == 1 {
		for accountType := range types {
			return accountType
		}
	}
	return "mixed"
}

func schedulerConfig(source sourceGroup, accountMap map[int64]int) string {
	config := map[string]any{
		"source": sourceSystemSub2API, "source_group_id": source.ID,
		"model_routing_enabled": source.ModelRoutingEnabled, "default_mapped_model": source.DefaultMappedModel,
	}
	if len(source.ModelRouting) > 0 {
		var sourceRouting map[string][]int64
		if common.Unmarshal(source.ModelRouting, &sourceRouting) == nil {
			if accountMap == nil {
				config["model_routing"] = sourceRouting
				config["model_routing_id_space"] = "source"
			} else {
				destinationRouting := make(map[string][]int64, len(sourceRouting))
				for pattern, sourceAccountIds := range sourceRouting {
					for _, sourceAccountId := range sourceAccountIds {
						if destinationAccountId, ok := accountMap[sourceAccountId]; ok {
							destinationRouting[pattern] = append(destinationRouting[pattern], int64(destinationAccountId))
						}
					}
				}
				config["model_routing"] = destinationRouting
				config["model_routing_id_space"] = "destination"
			}
		}
	}
	data, err := common.Marshal(config)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func uniqueImportedPoolName(db *gorm.DB, raw string, sourceID int64, currentID int) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		name = fmt.Sprintf("sub2api-group-%d", sourceID)
	}
	for attempt := 0; ; attempt++ {
		candidate := name
		if attempt == 1 {
			candidate = fmt.Sprintf("%s [sub2api:%d]", name, sourceID)
		} else if attempt > 1 {
			candidate = fmt.Sprintf("%s [sub2api:%d-%d]", name, sourceID, attempt)
		}
		var count int64
		query := db.Model(&model.UpstreamAccountPool{}).Where("name = ?", candidate)
		if currentID > 0 {
			query = query.Where("id <> ?", currentID)
		}
		if err := query.Count(&count).Error; err == nil && count == 0 {
			return candidate
		}
	}
}

func mapAccountType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "oauth":
		return constant.UpstreamAccountTypeOAuth
	case "api_key", "apikey":
		return constant.UpstreamAccountTypeAPIKey
	default:
		return ""
	}
}

func normalizeSourceAccountCredentials(accountType string, credentials map[string]any) map[string]any {
	normalized := make(map[string]any, len(credentials)+1)
	for key, value := range credentials {
		normalized[key] = value
	}
	if accountType != constant.UpstreamAccountTypeOAuth || credentialString(normalized["account_id"]) != "" {
		return normalized
	}
	if accountID := credentialString(normalized["chatgpt_account_id"]); accountID != "" {
		normalized["account_id"] = accountID
	}
	return normalized
}

func sourceAccountExpiresAt(source sourceAccount, accountType string, credentials map[string]any) *int64 {
	if source.ExpiresAt != nil {
		return unixPointer(source.ExpiresAt)
	}
	if accountType != constant.UpstreamAccountTypeOAuth {
		return nil
	}
	value, ok := credentials["expired"]
	if !ok || value == nil {
		return nil
	}
	var timestamp int64
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return nil
		}
		timestamp = parsed.Unix()
	case float64:
		timestamp = int64(typed)
	case int64:
		timestamp = typed
	case int:
		timestamp = int64(typed)
	default:
		return nil
	}
	if timestamp > 1_000_000_000_000 {
		timestamp /= 1000
	}
	if timestamp <= 0 {
		return nil
	}
	return &timestamp
}

func int64PointersEqual(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func credentialString(value any) string {
	if value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func mapActiveStatus(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "active") {
		return constant.UpstreamStatusActive
	}
	return constant.UpstreamStatusInactive
}

func mapAccountStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "active":
		return constant.UpstreamStatusActive
	case "error":
		return constant.UpstreamStatusError
	default:
		return constant.UpstreamStatusInactive
	}
}

func normalizeProxyProtocol(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case constant.UpstreamProxyProtocolHTTP, constant.UpstreamProxyProtocolHTTPS, constant.UpstreamProxyProtocolSOCKS5, constant.UpstreamProxyProtocolSOCKS5H:
		return value
	default:
		return constant.UpstreamProxyProtocolHTTP
	}
}

func validateSourceProxy(proxy sourceProxy) error {
	switch strings.ToLower(strings.TrimSpace(proxy.Protocol)) {
	case constant.UpstreamProxyProtocolHTTP, constant.UpstreamProxyProtocolHTTPS,
		constant.UpstreamProxyProtocolSOCKS5, constant.UpstreamProxyProtocolSOCKS5H:
	default:
		return fmt.Errorf("unsupported proxy protocol %q", proxy.Protocol)
	}
	switch strings.ToLower(strings.TrimSpace(proxy.FallbackMode)) {
	case "", constant.UpstreamProxyFallbackNone, constant.UpstreamProxyFallbackDirect:
		return nil
	case constant.UpstreamProxyFallbackProxy:
		if proxy.BackupProxyID == nil {
			return errors.New("proxy fallback requires backup_proxy_id")
		}
		return nil
	default:
		return fmt.Errorf("unsupported proxy fallback mode %q", proxy.FallbackMode)
	}
}

func normalizeFallbackMode(value string, hasBackup bool) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case constant.UpstreamProxyFallbackDirect:
		return constant.UpstreamProxyFallbackDirect
	case constant.UpstreamProxyFallbackProxy:
		if hasBackup {
			return constant.UpstreamProxyFallbackProxy
		}
	}
	return constant.UpstreamProxyFallbackNone
}

func mappedIntPointer(sourceID *int64, idMap map[int64]int) *int {
	if sourceID == nil {
		return nil
	}
	value, ok := idMap[*sourceID]
	if !ok {
		return nil
	}
	return &value
}

func unixPointer(value *time.Time) *int64 {
	if value == nil {
		return nil
	}
	timestamp := value.Unix()
	return &timestamp
}

func jsonText(value []byte) string {
	if len(value) == 0 {
		return "{}"
	}
	var object map[string]any
	if common.Unmarshal(value, &object) != nil {
		return "{}"
	}
	data, err := common.Marshal(object)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func stringPointer(value string) *string    { return &value }
func int64Pointer(value int64) *int64       { return &value }
func float64Pointer(value float64) *float64 { return &value }

func printReport(report migrationReport) error {
	data, err := common.Marshal(report)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
