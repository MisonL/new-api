package model

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestIsTransientDatabaseStartupError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "postgres recovery", err: errors.New("FATAL: the database system is starting up"), want: true},
		{name: "postgres recovery sqlstate", err: &pgconn.PgError{Code: "57P03", Message: "the database system is starting up"}, want: true},
		{name: "connection refused", err: errors.New("dial tcp 127.0.0.1:5432: connect: connection refused"), want: true},
		{name: "connection timeout", err: errors.New("i/o timeout"), want: true},
		{name: "checkpoint corruption", err: errors.New("replication checkpoint has wrong magic 0"), want: false},
		{name: "authentication failure", err: errors.New("password authentication failed for user"), want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, isTransientDatabaseStartupError(testCase.err))
		})
	}
}
