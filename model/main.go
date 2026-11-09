package model

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

var commonGroupCol string
var commonKeyCol string
var commonTrueVal string
var commonFalseVal string

var logKeyCol string
var logGroupCol string

// KeyColumn returns the dialect-quoted name of the Option table's key column.
//
// `key` is a reserved word in MySQL, so an unquoted "key = ?" predicate is a
// syntax error there (ERROR 1064) while PostgreSQL and SQLite accept it.
// Callers outside this package need the quoted spelling, which is why this is
// exported.
func KeyColumn() string {
	if commonKeyCol == "" {
		initCol()
	}
	return commonKeyCol
}

func initCol() {
	// init common column names
	if common.UsingPostgreSQL {
		commonGroupCol = `"group"`
		commonKeyCol = `"key"`
		commonTrueVal = "true"
		commonFalseVal = "false"
	} else {
		commonGroupCol = "`group`"
		commonKeyCol = "`key`"
		commonTrueVal = "1"
		commonFalseVal = "0"
	}
	if os.Getenv("LOG_SQL_DSN") != "" {
		// The log database can be a different engine than the main one, so its
		// column quoting has to follow the log database, not the main database.
		// Copying the main database's quoting produced `"group"` for a MySQL log
		// database under a PostgreSQL main database, and MySQL reads a
		// double-quoted name as a string literal unless ANSI_QUOTES is set, so the
		// predicate silently matched nothing. SQLite accepts backticks too, so the
		// non-PostgreSQL branch is safe for both remaining engines.
		//
		// logKeyCol is left alone: nothing reads it.
		switch common.LogSqlType {
		case common.DatabaseTypePostgreSQL:
			logGroupCol = `"group"`
			logKeyCol = `"key"`
		default:
			logGroupCol = "`group`"
			logKeyCol = commonKeyCol
		}
	} else {
		// LOG_SQL_DSN 为空时，日志数据库与主数据库相同
		if common.UsingPostgreSQL {
			logGroupCol = `"group"`
			logKeyCol = `"key"`
		} else {
			logGroupCol = commonGroupCol
			logKeyCol = commonKeyCol
		}
	}
	// log sql type and database type
	//common.SysLog("Using Log SQL Type: " + common.LogSqlType)
}

var DB *gorm.DB

var LOG_DB *gorm.DB

// forUpdate takes a row lock on the rows the following query selects.
//
// The previous spelling, Set("gorm:query_option", "FOR UPDATE"), is a GORM v1
// statement setting. GORM v2 has no reader for that key, so it emitted plain
// SELECTs and every "locked" read ran without a lock -- concurrent payment
// callbacks could each read the same pending order and each credit it. Use this
// helper instead of hand-writing the clause so the SQLite special case cannot
// be forgotten.
//
// SQLite has no row-level locking and its driver rejects the clause outright,
// so the transaction's own write lock is the only serialisation there; the
// surrounding transaction still makes the read-modify-write atomic.
func forUpdate(tx *gorm.DB) *gorm.DB {
	if tx == nil || tx.Dialector == nil || tx.Dialector.Name() == "sqlite" {
		return tx
	}
	return tx.Clauses(clause.Locking{Strength: "UPDATE"})
}

func createRootAccountIfNeed() error {
	var user User
	//if user.Status != common.UserStatusEnabled {
	if err := DB.First(&user).Error; err != nil {
		common.SysLog("no user exists, create a root user for you: username is root, password is 123456")
		hashedPassword, err := common.Password2Hash("123456")
		if err != nil {
			return err
		}
		rootUser := User{
			Username:    "root",
			Password:    hashedPassword,
			Role:        common.RoleRootUser,
			Status:      common.UserStatusEnabled,
			DisplayName: "Root User",
			AccessToken: nil,
			Quota:       100000000,
		}
		// 建号失败必须向上返回：吞掉错误时初始化流程认为 root 账号已就绪，
		// 之后所有 root 登录都会失败，且没有留下任何线索。
		if err := DB.Create(&rootUser).Error; err != nil {
			return fmt.Errorf("failed to create root user: %w", err)
		}
	}
	return nil
}

func CheckSetup() {
	setup := GetSetup()
	if setup == nil {
		// No setup record exists, check if we have a root user
		if RootUserExists() {
			common.SysLog("system is not initialized, but root user exists")
			// Create setup record
			newSetup := Setup{
				Version:       common.Version,
				InitializedAt: time.Now().Unix(),
			}
			err := DB.Create(&newSetup).Error
			if err != nil {
				common.SysLog("failed to create setup record: " + err.Error())
			}
			constant.Setup = true
		} else {
			common.SysLog("system is not initialized and no root user exists")
			constant.Setup = false
		}
	} else {
		// Setup record exists, system is initialized
		common.SysLog("system is already initialized at: " + time.Unix(setup.InitializedAt, 0).String())
		constant.Setup = true
	}
}

func newGormLogger(writer io.Writer) gormlogger.Interface {
	return newGormLoggerWithSlowThreshold(writer, 200*time.Millisecond)
}

func backgroundDB() *gorm.DB {
	return DB.Session(&gorm.Session{
		Logger: newGormLoggerWithSlowThreshold(os.Stdout, 30*time.Second),
	})
}

func newGormLoggerWithSlowThreshold(writer io.Writer, slowThreshold time.Duration) gormlogger.Interface {
	return gormlogger.New(log.New(writer, "\r\n", log.LstdFlags), gormlogger.Config{
		SlowThreshold:             slowThreshold,
		LogLevel:                  gormlogger.Warn,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
		Colorful:                  false,
	})
}

func newGormConfig() *gorm.Config {
	return &gorm.Config{
		PrepareStmt: true,
		Logger:      newGormLogger(os.Stdout),
	}
}

func chooseDB(envName string, isLog bool) (*gorm.DB, error) {
	defer func() {
		initCol()
	}()
	dsn := os.Getenv(envName)
	if dsn != "" {
		if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
			// Use PostgreSQL
			common.SysLog("using PostgreSQL as database")
			if !isLog {
				common.UsingPostgreSQL = true
			} else {
				common.LogSqlType = common.DatabaseTypePostgreSQL
			}
			return gorm.Open(postgres.New(postgres.Config{
				DSN:                  dsn,
				PreferSimpleProtocol: true, // disables implicit prepared statement usage
			}), newGormConfig())
		}
		if strings.HasPrefix(dsn, "local") {
			common.SysLog("SQL_DSN not set, using SQLite as database")
			if !isLog {
				common.UsingSQLite = true
			} else {
				common.LogSqlType = common.DatabaseTypeSQLite
			}
			return gorm.Open(sqlite.Open(common.SQLitePath), newGormConfig())
		}
		// Use MySQL
		common.SysLog("using MySQL as database")
		// check parseTime
		if !strings.Contains(dsn, "parseTime") {
			if strings.Contains(dsn, "?") {
				dsn += "&parseTime=true"
			} else {
				dsn += "?parseTime=true"
			}
		}
		if !isLog {
			common.UsingMySQL = true
		} else {
			common.LogSqlType = common.DatabaseTypeMySQL
		}
		return gorm.Open(mysqlOpenDialector(dsn), newGormConfig())
	}
	// Use SQLite
	common.SysLog("SQL_DSN not set, using SQLite as database")
	common.UsingSQLite = true
	return gorm.Open(sqlite.Open(common.SQLitePath), newGormConfig())
}

// 数据库连接池的默认上限。
//
// 默认值必须能被一台默认配置的 PostgreSQL 接住：stock PG 的 max_connections 是
// 100，其中 3 个预留给超级用户，非超级用户可用 97。请求数超过池上限时，
// database/sql 会让调用方排队；但池上限高于服务端容量时，超出的连接会直接被
// 拒绝（"too many clients already"），排队就变成了报错。留出余量给管理会话和
// 其他客户端，因此默认取 50。服务端已调优的部署用 SQL_MAX_OPEN_CONNS 显式抬高。
const (
	defaultSQLMaxIdleConns = 25
	defaultSQLMaxOpenConns = 50
	// A container can start before PostgreSQL has completed crash recovery. Keep
	// the retry bounded and only use it for connection/readiness errors; schema
	// and authentication failures must still fail startup immediately.
	defaultSQLStartupRetryAttempts     = 10
	defaultSQLStartupRetryIntervalSecs = 2
)

func isTransientDatabaseStartupError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "08000", "08001", "08003", "08004", "08006", "57P01", "57P03", "53300":
			return true
		}
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"i/o timeout",
		"database system is starting up",
		"database system is in recovery mode",
		"server is not ready",
		"server has gone away",
		"connection unexpectedly closed",
		"too many clients already",
		"too many connections",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func closeUnusableDB(db *gorm.DB) {
	if db == nil {
		return
	}
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}

func chooseDBWithStartupRetry(envName string, isLog bool) (*gorm.DB, error) {
	attempts := common.GetEnvOrDefault("SQL_STARTUP_RETRY_ATTEMPTS", defaultSQLStartupRetryAttempts)
	if attempts < 1 {
		attempts = 1
	}
	interval := time.Duration(common.GetEnvOrDefault(
		"SQL_STARTUP_RETRY_INTERVAL_SECONDS",
		defaultSQLStartupRetryIntervalSecs,
	)) * time.Second
	if interval < 0 {
		interval = 0
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		db, err := chooseDB(envName, isLog)
		if err == nil {
			return db, nil
		}
		closeUnusableDB(db)
		lastErr = err
		if !isTransientDatabaseStartupError(err) || attempt == attempts {
			break
		}
		common.SysError(fmt.Sprintf(
			"database %s is temporarily unavailable (attempt %d/%d), retrying in %s: %v",
			envName, attempt, attempts, interval, err,
		))
		time.Sleep(interval)
	}
	return nil, lastErr
}

func InitDB() (err error) {
	db, err := chooseDBWithStartupRetry("SQL_DSN", false)
	if err == nil {
		if common.DebugEnabled {
			db = db.Debug()
		}
		DB = db
		// MySQL charset/collation startup check: ensure Chinese-capable charset
		if common.UsingMySQL {
			if err := checkMySQLChineseSupport(DB); err != nil {
				panic(err)
			}
		}
		sqlDB, err := DB.DB()
		if err != nil {
			return err
		}
		sqlDB.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", defaultSQLMaxIdleConns))
		sqlDB.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", defaultSQLMaxOpenConns))
		sqlDB.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60)))

		if !common.IsMasterNode {
			return nil
		}
		if common.UsingMySQL {
			//_, _ = sqlDB.Exec("ALTER TABLE channels MODIFY model_mapping TEXT;") // TODO: delete this line when most users have upgraded
		}
		err = runDBMigration("main", DB, migrateDB)
		return err
	} else {
		common.FatalLog(err)
	}
	return err
}

func InitLogDB() (err error) {
	if os.Getenv("LOG_SQL_DSN") == "" {
		LOG_DB = DB
		if !common.IsMasterNode {
			return nil
		}
		return migrateLogIndexCleanup()
	}
	db, err := chooseDBWithStartupRetry("LOG_SQL_DSN", true)
	if err == nil {
		if common.DebugEnabled {
			db = db.Debug()
		}
		LOG_DB = db
		// If log DB is MySQL, also ensure Chinese-capable charset
		if common.LogSqlType == common.DatabaseTypeMySQL {
			if err := checkMySQLChineseSupport(LOG_DB); err != nil {
				panic(err)
			}
		}
		sqlDB, err := LOG_DB.DB()
		if err != nil {
			return err
		}
		sqlDB.SetMaxIdleConns(common.GetEnvOrDefault("SQL_MAX_IDLE_CONNS", defaultSQLMaxIdleConns))
		sqlDB.SetMaxOpenConns(common.GetEnvOrDefault("SQL_MAX_OPEN_CONNS", defaultSQLMaxOpenConns))
		sqlDB.SetConnMaxLifetime(time.Second * time.Duration(common.GetEnvOrDefault("SQL_MAX_LIFETIME", 60)))

		if !common.IsMasterNode {
			return nil
		}
		err = runDBMigration("log", LOG_DB, migrateLOGDB)
		return err
	} else {
		common.FatalLog(err)
	}
	return err
}

func runDBMigration(name string, db *gorm.DB, migrate func() error) error {
	start := time.Now()
	common.SysLog("database migration started: " + name)
	previousLogger := db.Config.Logger
	db.Config.Logger = newGormLoggerWithSlowThreshold(os.Stdout, 2*time.Second)
	defer func() {
		db.Config.Logger = previousLogger
	}()
	if err := migrate(); err != nil {
		common.SysLog(fmt.Sprintf("database migration failed: %s in %s: %v", name, time.Since(start).Round(time.Millisecond), err))
		return err
	}
	common.SysLog(fmt.Sprintf("database migration finished: %s in %s", name, time.Since(start).Round(time.Millisecond)))
	return nil
}

// autoMigrateModels 是启动时统一迁移的模型列表。
//
// 有一类表不能放进这里：表上带唯一索引、且索引之前必须先归并或改写存量行，
// 否则统一迁移会按标签抢先把索引建出来，存量数据让建索引失败并打断启动。
// Model/Vendor（MySQL 上 where:deleted_at IS NULL 被静默丢弃）、QuotaData
// （重复小时桶）、PrefillGroup 都由各自的专用迁移负责建表、同步列与建索引。
var autoMigrateModels = []interface{}{
	&Channel{},
	&Token{},
	&User{},
	&PasskeyCredential{},
	&Option{},
	&TagRequestHeaderPolicy{},
	&RequestHeaderStrategyState{},
	&SyntheticCompactStateRecord{},
	&UserHeaderTemplate{},
	&Redemption{},
	&GiftCode{},
	&InvitationRecord{},
	&Ability{},
	&Log{},
	&Midjourney{},
	&TopUp{},
	&Task{},
	&Setup{},
	&TwoFA{},
	&TwoFABackupCode{},
	&Checkin{},
	&SubscriptionOrder{},
	&UserSubscription{},
	&SubscriptionPreConsumeRecord{},
	&CustomOAuthProvider{},
	&UserOAuthBinding{},
	&PerfMetric{},
}

func migrateDB() error {
	// Migrate price_amount column from float/double to decimal for existing tables
	migrateSubscriptionPlanPriceAmount()
	// Migrate model_limits column from varchar to text for existing tables
	if err := migrateTokenModelLimitsToText(); err != nil {
		return err
	}
	if err := migrateRequestHeaderStrategyStateScopeTypeLength(); err != nil {
		return err
	}
	// logs 大表的缺失索引先并发建出，AutoMigrate 随后跳过已存在项。
	if err := ensureLogIndexesMatchModel(DB); err != nil {
		return err
	}

	if err := DB.AutoMigrate(autoMigrateModels...); err != nil {
		return err
	}
	if err := migrateChannelPinOrder(); err != nil {
		return err
	}
	if err := migrateRetryStatusCodesLegacyDefault(); err != nil {
		return err
	}
	if err := migratePrefillGroupTable(); err != nil {
		return err
	}
	if err := migrateModelMetaUniqueIndex(); err != nil {
		return err
	}
	if err := migrateVendorMetaUniqueIndex(); err != nil {
		return err
	}
	// QuotaData 不进上面的大 AutoMigrate：标签声明的唯一索引必须在合并存量
	// 重复桶之后才能建，表的创建与列同步都由这个专用迁移负责。
	if err := migrateQuotaDataUniqueIndex(); err != nil {
		return err
	}
	if err := migrateResponsesCompactModeAuto(); err != nil {
		return err
	}
	if common.UsingSQLite {
		if err := ensureSubscriptionPlanTableSQLite(); err != nil {
			return err
		}
	} else {
		if err := DB.AutoMigrate(&SubscriptionPlan{}); err != nil {
			return err
		}
	}
	return nil
}

func migrateLOGDB() error {
	var err error
	// 独立日志库同样先并发建缺失索引，再交给 AutoMigrate。
	if err = ensureLogIndexesMatchModel(LOG_DB); err != nil {
		return err
	}
	if err = LOG_DB.AutoMigrate(&Log{}); err != nil {
		return err
	}
	if err = migrateLogIndexCleanup(); err != nil {
		return err
	}
	return nil
}

const channelPinOrderMigrationOptionKey = "migration.channel_pin_order.20260829"

// migrateChannelPinOrder preserves the legacy management pin state exactly
// once, before pin_order becomes independent from route priority. Only
// positive legacy priorities represented a pinned channel in the old UI.
func migrateChannelPinOrder() error {
	var marker Option
	if err := DB.First(&marker, commonKeyCol+" = ?", channelPinOrderMigrationOptionKey).Error; err == nil {
		return nil
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&Channel{}).
			Where("(pin_order IS NULL OR pin_order = ?) AND priority > ?", 0, 0).
			Update("pin_order", gorm.Expr("priority"))
		if result.Error != nil {
			return result.Error
		}

		marker = Option{
			Key:   channelPinOrderMigrationOptionKey,
			Value: "done",
		}
		if err := tx.Save(&marker).Error; err != nil {
			return err
		}
		common.SysLog(fmt.Sprintf(
			"channel pin order migration finished: updated_channels=%d",
			result.RowsAffected,
		))
		return nil
	})
}

const retryStatusCodesLegacyDefaultMigrationOptionKey = "migration.automatic_retry_status_codes.20261101"

// retryStatusCodesLegacyDefaults 是历史出厂默认值的全部写法。这些值在数据库里
// 与"管理员显式自定义"无法区分，但都恰好等于旧版代码的出厂规则，可以安全升级。
// 启动时 loadOptionsFromDatabase 会用数据库行覆盖代码默认值，所以只改代码常量
// 对已经保存过设置的实例（包括只是点过一次保存的管理员）不生效，必须在启动
// 迁移里把原样的旧出厂值升到新出厂规则。
var retryStatusCodesLegacyDefaults = []string{
	"100-199,300-399,401-407,409-499,500-503,505-523,525-599",
	"100-199,300-399,400-407,409-499,500-503,505-523,525-599",
}

// migrateRetryStatusCodesLegacyDefault 把仍保存着旧出厂重试范围的行一次性
// 升级为新出厂规则（除 2xx 外全部可重试）。标记行保证只执行一次：迁移之后
// 管理员再手动填写旧范围（或其他自定义值）都会被视为运营决定，不再改写。
//
// 标记写入用 ON CONFLICT DO NOTHING 做成幂等：两个 master 同时启动时都可能
// 先读到"无标记"再进事务，后写入者若以主键冲突报错，runDBMigration 会阻断
// 整个进程启动。
func migrateRetryStatusCodesLegacyDefault() error {
	var marker Option
	if err := DB.First(&marker, KeyColumn()+" = ?", retryStatusCodesLegacyDefaultMigrationOptionKey).Error; err == nil {
		return nil
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		return applyRetryStatusCodesLegacyDefault(tx)
	})
}

// applyRetryStatusCodesLegacyDefault 是迁移的事务主体。标记检查在事务外，
// 这里不再重复检查，只保证自身幂等：并发重复执行时选项升级与标记写入都不报错。
func applyRetryStatusCodesLegacyDefault(tx *gorm.DB) error {
	updated := 0
	var option Option
	err := tx.Where(KeyColumn()+" = ?", "AutomaticRetryStatusCodes").Limit(1).Find(&option).Error
	if err != nil {
		return err
	}
	if option.Key != "" {
		for _, legacy := range retryStatusCodesLegacyDefaults {
			if option.Value != legacy {
				continue
			}
			if err := tx.Model(&Option{}).Where(KeyColumn()+" = ?", "AutomaticRetryStatusCodes").
				Update("value", operation_setting.DefaultAutomaticRetryStatusCodes()).Error; err != nil {
				return err
			}
			updated = 1
			break
		}
	}

	marker := Option{
		Key:   retryStatusCodesLegacyDefaultMigrationOptionKey,
		Value: "done",
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&marker).Error; err != nil {
		return err
	}
	common.SysLog(fmt.Sprintf("automatic retry status codes migration finished: updated_options=%d", updated))
	return nil
}

const (
	responsesCompactModeAutoMigrationOptionKey            = "migration.responses_compact_mode_auto.20260526"
	responsesCompactModeAutoAllChannelsMigrationOptionKey = "migration.responses_compact_mode_auto_all_channels.20260610"
	responsesCompactModeAutoMigrationBatchSize            = 100
)

func migrateResponsesCompactModeAuto() error {
	var allChannelsMarker Option
	if err := DB.First(&allChannelsMarker, commonKeyCol+" = ?", responsesCompactModeAutoAllChannelsMigrationOptionKey).Error; err == nil {
		return nil
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	if err := migrateResponsesCompactModeAutoWithMarker(responsesCompactModeAutoMigrationOptionKey, true); err != nil {
		return err
	}
	return migrateResponsesCompactModeAutoWithMarker(responsesCompactModeAutoAllChannelsMigrationOptionKey, false)
}

func migrateResponsesCompactModeAutoWithMarker(markerKey string, openAIOnly bool) error {
	var marker Option
	if err := DB.First(&marker, commonKeyCol+" = ?", markerKey).Error; err == nil {
		return nil
	} else if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		var channels []Channel
		updated := 0
		resetInvalid := 0
		query := tx.Model(&Channel{})
		if openAIOnly {
			query = query.Where("type = ?", constant.ChannelTypeOpenAI)
		}
		if err := query.FindInBatches(&channels, responsesCompactModeAutoMigrationBatchSize, func(_ *gorm.DB, _ int) error {
			for i := range channels {
				channel := &channels[i]
				settings := dto.ChannelOtherSettings{}
				if channel.OtherSettings != "" {
					if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
						common.SysLog(fmt.Sprintf(
							"Warning: reset invalid channel settings during responses compact auto migration: channel_id=%d error=%v",
							channel.Id,
							err,
						))
						settings = dto.ChannelOtherSettings{}
						resetInvalid++
					}
				}
				settings.ResponsesCompactMode = dto.ResponsesCompactModeAuto
				settings.ResponsesCompactAutoFallbackDate = 0
				settings.ResponsesCompactAutoFallbackAt = 0
				settings.ResponsesCompactAutoFallbackReason = ""
				raw, err := common.Marshal(settings)
				if err != nil {
					return fmt.Errorf("failed to marshal channel %d settings for responses compact auto migration: %w", channel.Id, err)
				}
				if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).Update("settings", string(raw)).Error; err != nil {
					return err
				}
				updated++
			}
			return nil
		}).Error; err != nil {
			return err
		}
		marker = Option{
			Key:   markerKey,
			Value: "done",
		}
		// 与 retryStatusCodes 迁移一致：两个 master 同时启动时都可能先读到
		// "无标记"再进事务，后写入者若以主键冲突报错，runDBMigration 会阻断
		// 整个进程启动。标记写入必须是幂等的。
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&marker).Error; err != nil {
			return err
		}
		common.SysLog(fmt.Sprintf("responses compact auto migration finished: marker=%s updated_channels=%d reset_invalid_settings=%d", markerKey, updated, resetInvalid))
		return nil
	})
}

type sqliteColumnDef struct {
	Name string
	DDL  string
}

func ensureSubscriptionPlanTableSQLite() error {
	if !common.UsingSQLite {
		return nil
	}
	tableName := "subscription_plans"
	if !DB.Migrator().HasTable(tableName) {
		createSQL := `CREATE TABLE ` + "`" + tableName + "`" + ` (
` + "`id`" + ` integer,
` + "`title`" + ` varchar(128) NOT NULL,
` + "`subtitle`" + ` varchar(255) DEFAULT '',
` + "`price_amount`" + ` decimal(10,6) NOT NULL,
` + "`currency`" + ` varchar(8) NOT NULL DEFAULT 'USD',
` + "`duration_unit`" + ` varchar(16) NOT NULL DEFAULT 'month',
` + "`duration_value`" + ` integer NOT NULL DEFAULT 1,
` + "`custom_seconds`" + ` bigint NOT NULL DEFAULT 0,
` + "`enabled`" + ` numeric DEFAULT 1,
` + "`sort_order`" + ` integer DEFAULT 0,
` + "`stripe_price_id`" + ` varchar(128) DEFAULT '',
` + "`creem_product_id`" + ` varchar(128) DEFAULT '',
` + "`max_purchase_per_user`" + ` integer DEFAULT 0,
` + "`upgrade_group`" + ` varchar(64) DEFAULT '',
` + "`total_amount`" + ` bigint NOT NULL DEFAULT 0,
` + "`quota_reset_period`" + ` varchar(16) DEFAULT 'never',
` + "`quota_reset_custom_seconds`" + ` bigint DEFAULT 0,
` + "`created_at`" + ` bigint,
` + "`updated_at`" + ` bigint,
PRIMARY KEY (` + "`id`" + `)
)`
		return DB.Exec(createSQL).Error
	}
	var cols []struct {
		Name string `gorm:"column:name"`
	}
	if err := DB.Raw("PRAGMA table_info(`" + tableName + "`)").Scan(&cols).Error; err != nil {
		return err
	}
	existing := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		existing[c.Name] = struct{}{}
	}
	required := []sqliteColumnDef{
		{Name: "title", DDL: "`title` varchar(128) NOT NULL"},
		{Name: "subtitle", DDL: "`subtitle` varchar(255) DEFAULT ''"},
		{Name: "price_amount", DDL: "`price_amount` decimal(10,6) NOT NULL"},
		{Name: "currency", DDL: "`currency` varchar(8) NOT NULL DEFAULT 'USD'"},
		{Name: "duration_unit", DDL: "`duration_unit` varchar(16) NOT NULL DEFAULT 'month'"},
		{Name: "duration_value", DDL: "`duration_value` integer NOT NULL DEFAULT 1"},
		{Name: "custom_seconds", DDL: "`custom_seconds` bigint NOT NULL DEFAULT 0"},
		{Name: "enabled", DDL: "`enabled` numeric DEFAULT 1"},
		{Name: "sort_order", DDL: "`sort_order` integer DEFAULT 0"},
		{Name: "stripe_price_id", DDL: "`stripe_price_id` varchar(128) DEFAULT ''"},
		{Name: "creem_product_id", DDL: "`creem_product_id` varchar(128) DEFAULT ''"},
		{Name: "max_purchase_per_user", DDL: "`max_purchase_per_user` integer DEFAULT 0"},
		{Name: "upgrade_group", DDL: "`upgrade_group` varchar(64) DEFAULT ''"},
		{Name: "total_amount", DDL: "`total_amount` bigint NOT NULL DEFAULT 0"},
		{Name: "quota_reset_period", DDL: "`quota_reset_period` varchar(16) DEFAULT 'never'"},
		{Name: "quota_reset_custom_seconds", DDL: "`quota_reset_custom_seconds` bigint DEFAULT 0"},
		{Name: "created_at", DDL: "`created_at` bigint"},
		{Name: "updated_at", DDL: "`updated_at` bigint"},
	}
	for _, col := range required {
		if _, ok := existing[col.Name]; ok {
			continue
		}
		if err := DB.Exec("ALTER TABLE `" + tableName + "` ADD COLUMN " + col.DDL).Error; err != nil {
			return err
		}
	}
	return nil
}

// migrateTokenModelLimitsToText migrates model_limits column from varchar(1024) to text
// This is safe to run multiple times - it checks the column type first
func migrateTokenModelLimitsToText() error {
	// SQLite uses type affinity, so TEXT and VARCHAR are effectively the same - no migration needed
	if common.UsingSQLite {
		return nil
	}

	tableName := "tokens"
	columnName := "model_limits"

	if !DB.Migrator().HasTable(tableName) {
		return nil
	}

	if !DB.Migrator().HasColumn(&Token{}, columnName) {
		return nil
	}

	var alterSQL string
	if common.UsingPostgreSQL {
		var dataType string
		if err := DB.Raw(`SELECT data_type FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&dataType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if dataType == "text" {
			return nil
		}
		alterSQL = fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s TYPE text`, tableName, columnName)
	} else if common.UsingMySQL {
		var columnType string
		if err := DB.Raw(`SELECT COLUMN_TYPE FROM information_schema.columns
				WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&columnType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if strings.ToLower(columnType) == "text" {
			return nil
		}
		alterSQL = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s text", tableName, columnName)
	} else {
		return nil
	}

	if alterSQL != "" {
		if err := DB.Exec(alterSQL).Error; err != nil {
			return fmt.Errorf("failed to migrate %s.%s to text: %w", tableName, columnName, err)
		}
		common.SysLog(fmt.Sprintf("Successfully migrated %s.%s to text", tableName, columnName))
	}
	return nil
}

// migrateSubscriptionPlanPriceAmount migrates price_amount column from float/double to decimal(10,6)
// This is safe to run multiple times - it checks the column type first
func migrateSubscriptionPlanPriceAmount() {
	// SQLite doesn't support ALTER COLUMN, and its type affinity handles this automatically
	// Skip early to avoid GORM parsing the existing table DDL which may cause issues
	if common.UsingSQLite {
		return
	}

	tableName := "subscription_plans"
	columnName := "price_amount"

	// Check if table exists first
	if !DB.Migrator().HasTable(tableName) {
		return
	}

	// Check if column exists
	if !DB.Migrator().HasColumn(&SubscriptionPlan{}, columnName) {
		return
	}

	var alterSQL string
	if common.UsingPostgreSQL {
		// PostgreSQL: Check if already decimal/numeric
		var dataType string
		if err := DB.Raw(`SELECT data_type FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&dataType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if dataType == "numeric" {
			return // Already decimal/numeric
		}
		alterSQL = fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s TYPE decimal(10,6) USING %s::decimal(10,6)`,
			tableName, columnName, columnName)
	} else if common.UsingMySQL {
		// MySQL: Check if already decimal
		var columnType string
		if err := DB.Raw(`SELECT COLUMN_TYPE FROM information_schema.columns
				WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&columnType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
		} else if strings.HasPrefix(strings.ToLower(columnType), "decimal") {
			return // Already decimal
		}
		alterSQL = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s decimal(10,6) NOT NULL DEFAULT 0",
			tableName, columnName)
	} else {
		return
	}

	if alterSQL != "" {
		if err := DB.Exec(alterSQL).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to migrate %s.%s to decimal: %v", tableName, columnName, err))
		} else {
			common.SysLog(fmt.Sprintf("Successfully migrated %s.%s to decimal(10,6)", tableName, columnName))
		}
	}
}

func closeDB(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	err = sqlDB.Close()
	return err
}

func CloseDB() error {
	if LOG_DB != DB {
		err := closeDB(LOG_DB)
		if err != nil {
			return err
		}
	}
	return closeDB(DB)
}

// checkMySQLChineseSupport ensures the MySQL connection and current schema
// default charset/collation can store Chinese characters. It allows common
// Chinese-capable charsets (utf8mb4, utf8, gbk, big5, gb18030) and panics otherwise.
func checkMySQLChineseSupport(db *gorm.DB) error {
	// 仅检测：当前库默认字符集/排序规则 + 各表的排序规则（隐含字符集）

	// Read current schema defaults
	var schemaCharset, schemaCollation string
	err := db.Raw("SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = DATABASE()").Row().Scan(&schemaCharset, &schemaCollation)
	if err != nil {
		return fmt.Errorf("读取当前库默认字符集/排序规则失败 / Failed to read schema default charset/collation: %v", err)
	}

	toLower := func(s string) string { return strings.ToLower(s) }
	// Allowed charsets that can store Chinese text
	allowedCharsets := map[string]string{
		"utf8mb4": "utf8mb4_",
		"utf8":    "utf8_",
		"gbk":     "gbk_",
		"big5":    "big5_",
		"gb18030": "gb18030_",
	}
	isChineseCapable := func(cs, cl string) bool {
		csLower := toLower(cs)
		clLower := toLower(cl)
		if prefix, ok := allowedCharsets[csLower]; ok {
			if clLower == "" {
				return true
			}
			return strings.HasPrefix(clLower, prefix)
		}
		// 如果仅提供了排序规则，尝试按排序规则前缀判断
		for _, prefix := range allowedCharsets {
			if strings.HasPrefix(clLower, prefix) {
				return true
			}
		}
		return false
	}

	// 1) 当前库默认值必须支持中文
	if !isChineseCapable(schemaCharset, schemaCollation) {
		return fmt.Errorf("当前库默认字符集/排序规则不支持中文：schema(%s/%s)。请将库设置为 utf8mb4/utf8/gbk/big5/gb18030 / Schema default charset/collation is not Chinese-capable: schema(%s/%s). Please set to utf8mb4/utf8/gbk/big5/gb18030",
			schemaCharset, schemaCollation, schemaCharset, schemaCollation)
	}

	// 2) 所有物理表的排序规则（隐含字符集）必须支持中文
	type tableInfo struct {
		Name      string
		Collation *string
	}
	var tables []tableInfo
	if err := db.Raw("SELECT TABLE_NAME, TABLE_COLLATION FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'").Scan(&tables).Error; err != nil {
		return fmt.Errorf("读取表排序规则失败 / Failed to read table collations: %v", err)
	}

	var badTables []string
	for _, t := range tables {
		// NULL 或空表示继承库默认设置，已在上面校验库默认，视为通过
		if t.Collation == nil || *t.Collation == "" {
			continue
		}
		cl := *t.Collation
		// 仅凭排序规则判断是否中文可用
		ok := false
		lower := strings.ToLower(cl)
		for _, prefix := range allowedCharsets {
			if strings.HasPrefix(lower, prefix) {
				ok = true
				break
			}
		}
		if !ok {
			badTables = append(badTables, fmt.Sprintf("%s(%s)", t.Name, cl))
		}
	}

	if len(badTables) > 0 {
		// 限制输出数量以避免日志过长
		maxShow := 20
		shown := badTables
		if len(shown) > maxShow {
			shown = shown[:maxShow]
		}
		return fmt.Errorf(
			"存在不支持中文的表，请修复其排序规则/字符集。示例（最多展示 %d 项）：%v / Found tables not Chinese-capable. Please fix their collation/charset. Examples (showing up to %d): %v",
			maxShow, shown, maxShow, shown,
		)
	}
	return nil
}

var (
	lastPingTime time.Time
	pingMutex    sync.Mutex
)

func PingDB() error {
	pingMutex.Lock()
	defer pingMutex.Unlock()

	if time.Since(lastPingTime) < time.Second*10 {
		return nil
	}

	sqlDB, err := DB.DB()
	if err != nil {
		log.Printf("Error getting sql.DB from GORM: %v", err)
		return err
	}

	err = sqlDB.Ping()
	if err != nil {
		log.Printf("Error pinging DB: %v", err)
		return err
	}

	lastPingTime = time.Now()
	common.SysLog("Database pinged successfully")
	return nil
}
