package aws

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func relayTimeoutForTest() int { return common.RelayTimeout }

func setRelayTimeoutForTest(v int) { common.RelayTimeout = v }

// AWS SDK 只认传进去的 context。此前 newAwsInvokeContext 用
// context.Background() 派生，客户端断开或上游取消都无法传到 Bedrock 调用，
// 请求只能等 SDK 自己的重试耗尽。
func TestNewAwsInvokeContextPropagatesCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := newAwsInvokeContext(parent)
	defer cancel()

	require.NoError(t, ctx.Err())

	cancelParent()

	require.ErrorIs(t, ctx.Err(), context.Canceled,
		"cancelling the request context must cancel the AWS invoke context")
}

func TestNewAwsInvokeContextKeepsRelayDeadline(t *testing.T) {
	previous := relayTimeoutForTest()
	setRelayTimeoutForTest(7)
	t.Cleanup(func() { setRelayTimeoutForTest(previous) })

	ctx, cancel := newAwsInvokeContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	require.True(t, ok, "RELAY_TIMEOUT is set, the invoke context must carry a deadline")
	require.WithinDuration(t, time.Now().Add(7*time.Second), deadline, 2*time.Second)
}

// 无截止时间时也必须保留父 context 的取消（此前返回裸 Background）。
func TestNewAwsInvokeContextWithoutRelayTimeoutStillCancellable(t *testing.T) {
	previous := relayTimeoutForTest()
	setRelayTimeoutForTest(0)
	t.Cleanup(func() { setRelayTimeoutForTest(previous) })

	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := newAwsInvokeContext(parent)
	defer cancel()

	cancelParent()

	require.True(t, errors.Is(ctx.Err(), context.Canceled))
}
