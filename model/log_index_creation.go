package model

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// logs 在正式环境是十 GB 级、数百万行。AutoMigrate 对索引只按名字判断存在
// 与否：缺失索引用普通 CREATE INDEX 建（PostgreSQL 上整条语句持写锁，等于
// 一次写阻塞），列形状与模型声明不一致的索引则永远不会被修正。
// option:CONCURRENTLY 也不能写进 Log 的索引标签：MySQL 驱动没有覆盖
// CreateIndex，会把该选项原样拼进 DDL 生成非法语句。
//
// 因此在 AutoMigrate 之前先按模型声明对齐 logs 的索引：缺失的、列形状不一致
// 的、以及并发建索引失败留下的 INVALID 索引，都在这里删掉重建。PostgreSQL
// 上使用 CONCURRENTLY 不阻塞写入；MySQL / SQLite 走 migrator 的普通 DDL。
// 新库（logs 表尚不存在）不适用：表小，交给 AutoMigrate 一次建完更简单。
func ensureLogIndexesMatchModel(db *gorm.DB) error {
	if !db.Migrator().HasTable(&Log{}) {
		return nil
	}

	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&Log{}); err != nil {
		return err
	}

	tableColumns, err := tableColumnSet(db, stmt.Table)
	if err != nil {
		return err
	}

	for _, index := range stmt.Schema.ParseIndexes() {
		// 这一步跑在 AutoMigrate 之前：从很老的版本升级时，logs 可能还缺
		// 模型里的新列，此时建索引会报列不存在。缺列的索引留给
		// AutoMigrate 补完列后按普通路径建（新列尚无数据，代价小）。
		if !indexColumnsExist(index, tableColumns) {
			continue
		}
		existing, err := lookupIndexState(db, stmt.Table, index.Name)
		if err != nil {
			return err
		}
		if existing.exists && existing.valid && sameColumns(existing.columns, declaredIndexColumns(index)) {
			continue
		}
		if existing.exists {
			if err := dropLogIndex(db, index.Name); err != nil {
				return err
			}
		}
		if err := createLogIndex(db, stmt.Table, index); err != nil {
			return err
		}
	}
	return nil
}

func declaredIndexColumns(index *schema.Index) []string {
	columns := make([]string, 0, len(index.Fields))
	for _, field := range index.Fields {
		columns = append(columns, field.DBName)
	}
	return columns
}

func sameColumns(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}

func indexColumnsExist(index *schema.Index, columns map[string]struct{}) bool {
	for _, field := range index.Fields {
		if field.Expression != "" {
			// 表达式索引无法从字段名判断依赖，交给 AutoMigrate 处理。
			return false
		}
		if _, ok := columns[field.DBName]; !ok {
			return false
		}
	}
	return true
}

func tableColumnSet(db *gorm.DB, table string) (map[string]struct{}, error) {
	columns, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		set[column.Name()] = struct{}{}
	}
	return set, nil
}

type indexState struct {
	exists  bool
	valid   bool
	columns []string
}

// lookupIndexState 读取索引的存在性、可用性与列顺序。INVALID 索引（并发建
// 索引中途失败或被取消后的残留）在查询里完全不可用，只判断存在性会把它当成
// 已完成，之后每次启动都跳过。
func lookupIndexState(db *gorm.DB, table string, name string) (indexState, error) {
	switch {
	case isPostgreSQL(db):
		return lookupIndexStatePostgres(db, table, name)
	case db.Dialector.Name() == "sqlite":
		return lookupIndexStateSQLite(db, table, name)
	default:
		return lookupIndexStateMySQL(db, table, name)
	}
}

func lookupIndexStatePostgres(db *gorm.DB, table string, name string) (indexState, error) {
	var rows []struct {
		Valid bool
	}
	err := db.Raw(
		`SELECT i.indisvalid AS valid
		   FROM pg_index i
		   JOIN pg_class ic ON ic.oid = i.indexrelid
		   JOIN pg_class tc ON tc.oid = i.indrelid
		   JOIN pg_namespace n ON n.oid = tc.relnamespace
		  WHERE ic.relname = ? AND tc.relname = ? AND n.nspname = current_schema()`,
		name, table,
	).Scan(&rows).Error
	if err != nil {
		return indexState{}, err
	}
	if len(rows) == 0 {
		return indexState{}, nil
	}

	var columns []string
	if err := db.Raw(
		`SELECT a.attname
		   FROM pg_index i
		   JOIN unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
		   JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
		   JOIN pg_class ic ON ic.oid = i.indexrelid
		   JOIN pg_class tc ON tc.oid = i.indrelid
		   JOIN pg_namespace n ON n.oid = tc.relnamespace
		  WHERE ic.relname = ? AND tc.relname = ? AND n.nspname = current_schema()
		  ORDER BY k.ord`,
		name, table,
	).Scan(&columns).Error; err != nil {
		return indexState{}, err
	}
	return indexState{exists: true, valid: rows[0].Valid, columns: columns}, nil
}

func lookupIndexStateSQLite(db *gorm.DB, table string, name string) (indexState, error) {
	var count int64
	if err := db.Raw(
		"SELECT count(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND name = ?",
		table, name,
	).Scan(&count).Error; err != nil {
		return indexState{}, err
	}
	if count == 0 {
		return indexState{}, nil
	}
	var columns []string
	if err := db.Raw(
		"SELECT name FROM pragma_index_info(?) ORDER BY seqno", name,
	).Scan(&columns).Error; err != nil {
		return indexState{}, err
	}
	return indexState{exists: true, valid: true, columns: columns}, nil
}

func lookupIndexStateMySQL(db *gorm.DB, table string, name string) (indexState, error) {
	var currentDatabase string
	if err := db.Raw("SELECT DATABASE()").Scan(&currentDatabase).Error; err != nil {
		return indexState{}, err
	}
	var columns []string
	if err := db.Raw(
		`SELECT column_name FROM information_schema.statistics
		  WHERE table_schema = ? AND table_name = ? AND index_name = ?
		  ORDER BY seq_in_index`,
		currentDatabase, table, name,
	).Scan(&columns).Error; err != nil {
		return indexState{}, err
	}
	if len(columns) == 0 {
		return indexState{}, nil
	}
	return indexState{exists: true, valid: true, columns: columns}, nil
}

func dropLogIndex(db *gorm.DB, name string) error {
	if isPostgreSQL(db) {
		return dropLogIndexConcurrently(db, name)
	}
	return db.Migrator().DropIndex(&Log{}, name)
}

// createLogIndex 用与 gorm 相同的列定义建索引。PostgreSQL 上用
// CREATE INDEX CONCURRENTLY：CONCURRENTLY 不能进事务块、不能被预处理，
// 所以走 Exec 的字面 SQL；其他方言交给 migrator。
//
// 这里只支持 Log 当前用到的索引属性。where / comment 等属性如果将来被加到
// Log 上，直接报错而不是建成一个与模型声明不同的索引：AutoMigrate 只按
// 名字判断，形状不一致不会被发现。
func createLogIndex(db *gorm.DB, table string, index *schema.Index) error {
	if index.Where != "" || index.Comment != "" || index.Option != "" {
		return fmt.Errorf(
			"log index %s declares attributes (where/comment/option) the explicit creation path does not support",
			index.Name,
		)
	}

	if !isPostgreSQL(db) {
		return db.Migrator().CreateIndex(&Log{}, index.Name)
	}

	columns := make([]string, 0, len(index.Fields))
	for _, field := range index.Fields {
		column := quoteIdentifier(field.DBName)
		if field.Collate != "" {
			column += " COLLATE " + field.Collate
		}
		if field.Sort != "" {
			column += " " + field.Sort
		}
		columns = append(columns, column)
	}

	create := "CREATE "
	if index.Class != "" {
		create += index.Class + " "
	}
	create += "INDEX CONCURRENTLY IF NOT EXISTS " + quoteIdentifier(index.Name) +
		" ON " + quoteIdentifier(table)
	if index.Type != "" {
		create += " USING " + index.Type
	}
	create += " (" + strings.Join(columns, ", ") + ")"

	if err := db.Exec(create).Error; err != nil {
		// 多实例同时启动时，同名索引可能刚被另一个实例建完，PostgreSQL 对
		// 并发同名 CREATE INDEX 会报重复键。按最终状态判断：索引存在且可用
		// 就视为成功，其余错误原样返回。
		state, stateErr := lookupIndexState(db, table, index.Name)
		if stateErr == nil && state.exists && state.valid {
			return nil
		}
		return fmt.Errorf("create index %s concurrently: %w", index.Name, err)
	}
	return nil
}
