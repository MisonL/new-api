package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A connection pool whose ceiling exceeds the server's capacity does not
// degrade gracefully: once PostgreSQL's max_connections is reached, further
// connections are refused outright ("too many clients already") instead of
// queueing, so the surplus of the pool is not headroom -- it is a pool of
// guaranteed failures under load.
//
// A stock PostgreSQL allows 100 connections with 3 reserved for superusers,
// and nothing in this repository raises it: the bundled compose passes no
// -c max_connections, and both the production and dev instances run the image
// default. The app's own default therefore has to fit inside that budget.

// stockPostgresMaxConnections and its superuser reserve are PostgreSQL's
// documented defaults, which this repository's compose does not override.
const (
	stockPostgresMaxConnections  = 100
	postgresSuperuserReserved    = 3
	postgresNonSuperuserCapacity = stockPostgresMaxConnections - postgresSuperuserReserved
)

// TestDefaultSQLMaxOpenConnsFitsStockPostgres is the regression: the built-in
// pool ceiling must be usable against an unmodified PostgreSQL.
func TestDefaultSQLMaxOpenConnsFitsStockPostgres(t *testing.T) {
	require.LessOrEqual(t, defaultSQLMaxOpenConns, postgresNonSuperuserCapacity,
		"默认连接池上限 %d 超过 stock PostgreSQL 的非超级用户容量 %d，"+
			"超出部分会以 too many clients already 直接失败而不是排队",
		defaultSQLMaxOpenConns, postgresNonSuperuserCapacity)
}

// TestDefaultSQLMaxOpenConnsLeavesRoomForOthers keeps the default from being
// raised right up to the ceiling: the server also serves admin sessions,
// migrations and any other client.
func TestDefaultSQLMaxOpenConnsLeavesRoomForOthers(t *testing.T) {
	require.Less(t, defaultSQLMaxOpenConns, postgresNonSuperuserCapacity,
		"默认池上限不应占满服务端全部容量，需给管理会话与其他客户端留余量")
}

// TestDefaultSQLMaxIdleConnsDoesNotExceedOpen pins the internal consistency of
// the pair: an idle ceiling above the open ceiling is meaningless and
// database/sql clamps it, which silently hides a misconfiguration.
func TestDefaultSQLMaxIdleConnsDoesNotExceedOpen(t *testing.T) {
	require.LessOrEqual(t, defaultSQLMaxIdleConns, defaultSQLMaxOpenConns,
		"默认空闲连接上限不得高于默认连接上限")
}
