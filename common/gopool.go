package common

import (
	"context"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/constant"

	"github.com/bytedance/gopkg/util/gopool"
)

var relayGoPool gopool.Pool

func init() {
	relayGoPool = gopool.NewPool("gopool.RelayPool", math.MaxInt32, gopool.NewConfig())
	relayGoPool.SetPanicHandler(func(ctx context.Context, i interface{}) {
		if stopChan, ok := ctx.Value(constant.ContextKeyRelayStopChan).(chan bool); ok {
			SafeSendBool(stopChan, true)
		}
		SysError(fmt.Sprintf("panic in gopool.RelayPool: %v", i))
	})
}

func RelayCtxGo(ctx context.Context, f func()) {
	relayGoPool.CtxGo(ctx, f)
}
