package model

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const requestHeaderStrategyStateScopeTypeLength = 64

func migrateRequestHeaderStrategyStateScopeTypeLength() error {
	if common.UsingSQLite {
		return nil
	}

	tableName := "request_header_strategy_states"
	columnName := "scope_type"

	if !DB.Migrator().HasTable(tableName) {
		return nil
	}
	if !DB.Migrator().HasColumn(&RequestHeaderStrategyState{}, columnName) {
		return nil
	}

	alterSQL, err := requestHeaderStrategyStateScopeTypeAlterSQL(tableName, columnName)
	if err != nil || alterSQL == "" {
		return err
	}

	if err := DB.Exec(alterSQL).Error; err != nil {
		return fmt.Errorf("failed to migrate %s.%s to varchar(%d): %w", tableName, columnName, requestHeaderStrategyStateScopeTypeLength, err)
	}
	common.SysLog(fmt.Sprintf("Successfully migrated %s.%s to varchar(%d)", tableName, columnName, requestHeaderStrategyStateScopeTypeLength))
	return nil
}

func requestHeaderStrategyStateScopeTypeAlterSQL(tableName string, columnName string) (string, error) {
	if common.UsingPostgreSQL {
		needsMigration, err := postgresVarcharColumnNeedsLengthMigration(tableName, columnName, requestHeaderStrategyStateScopeTypeLength)
		if err != nil || !needsMigration {
			return "", err
		}
		return fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s TYPE varchar(%d)`, tableName, columnName, requestHeaderStrategyStateScopeTypeLength), nil
	}
	if common.UsingMySQL {
		needsMigration, err := mysqlVarcharColumnNeedsLengthMigration(tableName, columnName, requestHeaderStrategyStateScopeTypeLength)
		if err != nil || !needsMigration {
			return "", err
		}
		return fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s varchar(%d) NOT NULL", tableName, columnName, requestHeaderStrategyStateScopeTypeLength), nil
	}
	return "", nil
}

func postgresVarcharColumnNeedsLengthMigration(tableName string, columnName string, targetLength int) (bool, error) {
	var dataType string
	var maxLength sql.NullInt64
	err := DB.Raw(`SELECT data_type, character_maximum_length FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
		tableName, columnName).Row().Scan(&dataType, &maxLength)
	return varcharColumnNeedsLengthMigration(tableName, columnName, dataType, maxLength, targetLength, err, "character varying")
}

func mysqlVarcharColumnNeedsLengthMigration(tableName string, columnName string, targetLength int) (bool, error) {
	var dataType string
	var maxLength sql.NullInt64
	err := DB.Raw(`SELECT DATA_TYPE, CHARACTER_MAXIMUM_LENGTH FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
		tableName, columnName).Row().Scan(&dataType, &maxLength)
	return varcharColumnNeedsLengthMigration(tableName, columnName, dataType, maxLength, targetLength, err, "varchar")
}

func varcharColumnNeedsLengthMigration(
	tableName string,
	columnName string,
	dataType string,
	maxLength sql.NullInt64,
	targetLength int,
	queryErr error,
	expectedDataType string,
) (bool, error) {
	if queryErr == sql.ErrNoRows {
		return false, nil
	}
	if queryErr != nil {
		return false, fmt.Errorf("failed to query metadata for %s.%s: %w", tableName, columnName, queryErr)
	}
	if strings.ToLower(dataType) != expectedDataType || !maxLength.Valid {
		return false, nil
	}
	return maxLength.Int64 < int64(targetLength), nil
}
