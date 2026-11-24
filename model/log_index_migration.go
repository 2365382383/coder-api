package model

import (
	"fmt"
	"slices"
	"strings"

	"gorm.io/gorm"
)

const (
	usageLogStatsIndex        = "idx_logs_type_created_quota"
	legacyUsageLogTypeIndex   = "idx_created_at_type"
	usageLogStatsIndexColumns = "type, created_at, quota"
)

var expectedUsageLogStatsIndexColumns = []string{"type", "created_at", "quota"}

func migrateUsageLogStatsIndex(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("migrate usage log stats index: database is nil")
	}
	if db.Dialector.Name() == "clickhouse" {
		return nil
	}
	migrator := db.Migrator()
	if !migrator.HasTable(&Log{}) {
		return nil
	}

	desiredExists := migrator.HasIndex(&Log{}, usageLogStatsIndex)
	desiredValid := false
	if desiredExists {
		columns, err := inspectUsageLogStatsIndexColumns(db)
		if err != nil {
			return err
		}
		desiredValid = slices.Equal(columns, expectedUsageLogStatsIndexColumns)
		if desiredValid && db.Dialector.Name() == "postgres" {
			desiredValid, err = usageLogStatsIndexValid(db)
			if err != nil {
				return err
			}
		}
	}
	legacyExists := migrator.HasIndex(&Log{}, legacyUsageLogTypeIndex)
	if desiredValid && !legacyExists {
		return nil
	}

	switch db.Dialector.Name() {
	case "mysql":
		if err := migrateUsageLogStatsIndexMySQL(db, desiredExists, desiredValid, legacyExists); err != nil {
			return err
		}
	case "postgres":
		if err := migrateUsageLogStatsIndexPostgres(db, desiredExists, desiredValid, legacyExists); err != nil {
			return err
		}
	case "sqlite":
		if err := migrateUsageLogStatsIndexSQLite(db, desiredExists, desiredValid, legacyExists); err != nil {
			return err
		}
	default:
		return fmt.Errorf("migrate usage log stats index: unsupported database %q", db.Dialector.Name())
	}

	columns, err := inspectUsageLogStatsIndexColumns(db)
	if err != nil {
		return err
	}
	if !slices.Equal(columns, expectedUsageLogStatsIndexColumns) {
		return fmt.Errorf("usage log stats index %q has columns %v; want %v", usageLogStatsIndex, columns, expectedUsageLogStatsIndexColumns)
	}
	if db.Dialector.Name() == "postgres" {
		valid, err := usageLogStatsIndexValid(db)
		if err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("usage log stats index %q has an unexpected or invalid definition", usageLogStatsIndex)
		}
	}
	if migrator.HasIndex(&Log{}, legacyUsageLogTypeIndex) {
		return fmt.Errorf("legacy usage log index %q still exists after migration", legacyUsageLogTypeIndex)
	}
	return nil
}

func usageLogStatsIndexValid(db *gorm.DB) (bool, error) {
	var state struct {
		Valid bool `gorm:"column:valid"`
	}
	if err := db.Raw(`
SELECT COALESCE(bool_or(
           index_meta.indisvalid
           AND index_meta.indisready
           AND NOT index_meta.indisunique
           AND index_meta.indpred IS NULL
           AND index_meta.indexprs IS NULL
           AND index_meta.indnatts = 3
           AND access_method.amname = 'btree'
       ), false) AS valid
FROM pg_catalog.pg_index AS index_meta
JOIN pg_catalog.pg_class AS table_meta
  ON table_meta.oid = index_meta.indrelid
JOIN pg_catalog.pg_class AS index_class
  ON index_class.oid = index_meta.indexrelid
JOIN pg_catalog.pg_am AS access_method
  ON access_method.oid = index_class.relam
WHERE table_meta.oid = to_regclass('logs')
  AND index_class.relname = ?`, usageLogStatsIndex).Scan(&state).Error; err != nil {
		return false, fmt.Errorf("inspect usage log stats index definition: %w", err)
	}
	return state.Valid, nil
}

func inspectUsageLogStatsIndexColumns(db *gorm.DB) ([]string, error) {
	var columns []string
	var err error
	switch db.Dialector.Name() {
	case "mysql":
		err = db.Raw(`
SELECT COLUMN_NAME
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE()
  AND TABLE_NAME = 'logs'
  AND INDEX_NAME = ?
ORDER BY SEQ_IN_INDEX`, usageLogStatsIndex).Scan(&columns).Error
	case "postgres":
		err = db.Raw(`
SELECT attribute_meta.attname
FROM pg_catalog.pg_index AS index_meta
JOIN pg_catalog.pg_class AS table_meta
  ON table_meta.oid = index_meta.indrelid
JOIN pg_catalog.pg_class AS index_class
  ON index_class.oid = index_meta.indexrelid
JOIN LATERAL unnest(index_meta.indkey) WITH ORDINALITY AS key_column(attnum, ordinality)
  ON true
JOIN pg_catalog.pg_attribute AS attribute_meta
  ON attribute_meta.attrelid = table_meta.oid
 AND attribute_meta.attnum = key_column.attnum
WHERE table_meta.oid = to_regclass('logs')
  AND index_class.relname = ?
ORDER BY key_column.ordinality`, usageLogStatsIndex).Scan(&columns).Error
	case "sqlite":
		err = db.Raw("SELECT name FROM PRAGMA_index_info(?) ORDER BY seqno", usageLogStatsIndex).Scan(&columns).Error
	default:
		return nil, fmt.Errorf("inspect usage log stats index: unsupported database %q", db.Dialector.Name())
	}
	if err != nil {
		return nil, fmt.Errorf("inspect usage log stats index: %w", err)
	}
	for i := range columns {
		columns[i] = strings.ToLower(columns[i])
	}
	return columns, nil
}

func migrateUsageLogStatsIndexMySQL(db *gorm.DB, desiredExists, desiredValid, legacyExists bool) error {
	operations := make([]string, 0, 3)
	if desiredExists && !desiredValid {
		operations = append(operations, "DROP INDEX "+usageLogStatsIndex)
	}
	if !desiredValid {
		operations = append(operations, "ADD INDEX "+usageLogStatsIndex+" ("+usageLogStatsIndexColumns+")")
	}
	if legacyExists {
		operations = append(operations, "DROP INDEX "+legacyUsageLogTypeIndex)
	}
	if len(operations) == 0 {
		return nil
	}
	sql := "ALTER TABLE logs " + strings.Join(operations, ", ") + ", ALGORITHM=INPLACE, LOCK=NONE"
	if err := db.Exec(sql).Error; err != nil {
		return fmt.Errorf("migrate usage log stats index on mysql: %w", err)
	}
	return nil
}

func migrateUsageLogStatsIndexPostgres(db *gorm.DB, desiredExists, desiredValid, legacyExists bool) error {
	if desiredExists && !desiredValid {
		if err := db.Exec("DROP INDEX CONCURRENTLY IF EXISTS " + usageLogStatsIndex).Error; err != nil {
			return fmt.Errorf("drop invalid usage log stats index on postgres: %w", err)
		}
		desiredExists = false
	}
	if !desiredExists {
		if err := db.Exec("CREATE INDEX CONCURRENTLY IF NOT EXISTS " + usageLogStatsIndex + " ON logs (" + usageLogStatsIndexColumns + ")").Error; err != nil {
			return fmt.Errorf("create usage log stats index on postgres: %w", err)
		}
	}
	if legacyExists {
		if err := db.Exec("DROP INDEX CONCURRENTLY IF EXISTS " + legacyUsageLogTypeIndex).Error; err != nil {
			return fmt.Errorf("drop legacy usage log index on postgres: %w", err)
		}
	}
	return nil
}

func migrateUsageLogStatsIndexSQLite(db *gorm.DB, desiredExists, desiredValid, legacyExists bool) error {
	if desiredExists && !desiredValid {
		if err := db.Exec("DROP INDEX IF EXISTS " + usageLogStatsIndex).Error; err != nil {
			return fmt.Errorf("drop invalid usage log stats index on sqlite: %w", err)
		}
		desiredExists = false
	}
	if !desiredExists {
		if err := db.Exec("CREATE INDEX IF NOT EXISTS " + usageLogStatsIndex + " ON logs (" + usageLogStatsIndexColumns + ")").Error; err != nil {
			return fmt.Errorf("create usage log stats index on sqlite: %w", err)
		}
	}
	if legacyExists {
		if err := db.Exec("DROP INDEX IF EXISTS " + legacyUsageLogTypeIndex).Error; err != nil {
			return fmt.Errorf("drop legacy usage log index on sqlite: %w", err)
		}
	}
	return nil
}
