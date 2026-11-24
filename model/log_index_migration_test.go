package model

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	sqlmysql "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMigrateUsageLogStatsIndexSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	require.NoError(t, db.AutoMigrate(&Log{}))
	require.True(t, db.Migrator().HasIndex(&Log{}, usageLogStatsIndex))
	require.False(t, db.Migrator().HasIndex(&Log{}, legacyUsageLogTypeIndex))

	require.NoError(t, db.Exec("DROP INDEX "+usageLogStatsIndex).Error)
	require.NoError(t, db.Exec("CREATE INDEX "+legacyUsageLogTypeIndex+" ON logs (created_at, type)").Error)
	require.NoError(t, db.Create(&Log{Type: LogTypeConsume, CreatedAt: 100, Quota: 123}).Error)

	require.NoError(t, migrateUsageLogStatsIndex(db))
	require.NoError(t, migrateUsageLogStatsIndex(db))
	require.True(t, db.Migrator().HasIndex(&Log{}, usageLogStatsIndex))
	require.False(t, db.Migrator().HasIndex(&Log{}, legacyUsageLogTypeIndex))

	columns, err := inspectUsageLogStatsIndexColumns(db)
	require.NoError(t, err)
	require.Equal(t, expectedUsageLogStatsIndexColumns, columns)
	var count int64
	require.NoError(t, db.Model(&Log{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestMigrateUsageLogStatsIndexDatabaseMatrix(t *testing.T) {
	for _, testCase := range []struct {
		name string
		env  string
	}{
		{name: "sqlite"},
		{name: "mysql", env: "TEST_MYSQL_DSN"},
		{name: "postgres", env: "TEST_POSTGRES_DSN"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.env != "" && strings.TrimSpace(os.Getenv(testCase.env)) == "" {
				t.Skip(testCase.env + " is not configured")
			}
			for _, upgrade := range []bool{false, true} {
				t.Run(fmt.Sprintf("upgrade=%v", upgrade), func(t *testing.T) {
					db := openIsolatedUsageLogIndexTestDB(t, testCase.name, os.Getenv(testCase.env))
					require.NoError(t, db.AutoMigrate(&Log{}))
					if upgrade {
						require.NoError(t, db.Migrator().DropIndex(&Log{}, usageLogStatsIndex))
						require.NoError(t, db.Exec("CREATE INDEX "+legacyUsageLogTypeIndex+" ON logs (created_at, type)").Error)
						if testCase.name == "postgres" {
							require.NoError(t, db.Create([]Log{
								{Type: LogTypeConsume, CreatedAt: 100, Quota: 123},
								{Type: LogTypeConsume, CreatedAt: 100, Quota: 123},
							}).Error)
							require.Error(t, db.Exec("CREATE UNIQUE INDEX CONCURRENTLY "+usageLogStatsIndex+" ON logs (type, created_at, quota)").Error)
							valid, err := usageLogStatsIndexValid(db)
							require.NoError(t, err)
							require.False(t, valid, "failed concurrent unique-index build leaves an invalid index")
						} else {
							require.NoError(t, db.Create(&Log{Type: LogTypeConsume, CreatedAt: 100, Quota: 123}).Error)
						}
					}

					require.NoError(t, migrateUsageLogStatsIndex(db))
					require.NoError(t, migrateUsageLogStatsIndex(db))
					require.True(t, db.Migrator().HasIndex(&Log{}, usageLogStatsIndex))
					require.False(t, db.Migrator().HasIndex(&Log{}, legacyUsageLogTypeIndex))
					columns, err := inspectUsageLogStatsIndexColumns(db)
					require.NoError(t, err)
					require.Equal(t, expectedUsageLogStatsIndexColumns, columns)
					if upgrade {
						var count int64
						require.NoError(t, db.Model(&Log{}).Count(&count).Error)
						if testCase.name == "postgres" {
							require.EqualValues(t, 2, count)
							valid, err := usageLogStatsIndexValid(db)
							require.NoError(t, err)
							require.True(t, valid)
						} else {
							require.EqualValues(t, 1, count)
						}
					}
				})
			}
		})
	}
}

func openIsolatedUsageLogIndexTestDB(t *testing.T, kind, dsn string) *gorm.DB {
	t.Helper()
	if kind == "sqlite" {
		db, err := gorm.Open(sqlite.Open(t.TempDir()+"/logs.db"), &gorm.Config{})
		require.NoError(t, err)
		return db
	}

	databaseName := fmt.Sprintf("newapi_log_idx_%d", time.Now().UnixNano())
	var openDialector func(string) gorm.Dialector
	var adminDSN, isolatedDSN string
	switch kind {
	case "mysql":
		config, err := sqlmysql.ParseDSN(dsn)
		require.NoError(t, err)
		require.Equal(t, "tcp", config.Net)
		host, _, err := net.SplitHostPort(config.Addr)
		require.NoError(t, err)
		require.True(t, net.ParseIP(host).IsLoopback(), "database tests only permit loopback instances")
		adminDSN = dsn
		config.DBName = databaseName
		isolatedDSN = config.FormatDSN()
		openDialector = func(value string) gorm.Dialector { return mysql.Open(value) }
	case "postgres":
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.True(t, net.ParseIP(parsed.Hostname()).IsLoopback(), "database tests only permit loopback instances")
		adminDSN = dsn
		parsed.Path = "/" + databaseName
		isolatedDSN = parsed.String()
		openDialector = func(value string) gorm.Dialector {
			return postgres.New(postgres.Config{DSN: value, PreferSimpleProtocol: true})
		}
	default:
		t.Fatalf("unsupported database matrix kind %q", kind)
	}

	admin, err := gorm.Open(openDialector(adminDSN), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, admin.Exec("CREATE DATABASE "+databaseName).Error)
	adminSQL, err := admin.DB()
	require.NoError(t, err)
	require.NoError(t, adminSQL.Close())

	db, err := gorm.Open(openDialector(isolatedDSN), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		cleanup, err := gorm.Open(openDialector(adminDSN), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, cleanup.Exec("DROP DATABASE "+databaseName).Error)
		cleanupSQL, err := cleanup.DB()
		require.NoError(t, err)
		require.NoError(t, cleanupSQL.Close())
	})
	return db
}
