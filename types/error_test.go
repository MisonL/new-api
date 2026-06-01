package types

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsUpstreamTransportInterruptedError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "unexpected eof sentinel",
			err:  io.ErrUnexpectedEOF,
			want: true,
		},
		{
			name: "http2 goaway",
			err:  errors.New("http2: server sent GOAWAY and closed the connection"),
			want: true,
		},
		{
			name: "unexpected eof message",
			err:  errors.New("unexpected EOF"),
			want: true,
		},
		{
			name: "connection reset by peer",
			err:  errors.New("read tcp 127.0.0.1:1->127.0.0.1:2: connection reset by peer"),
			want: true,
		},
		{
			name: "dial timeout",
			err:  errors.New("dial tcp timeout"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, IsUpstreamTransportInterruptedError(tc.err))
		})
	}
}
