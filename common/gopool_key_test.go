package common

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
)

// The relay stream's stop channel travels from relay/helper into the gopool
// panic handler through the request context. Both sides must use the shared
// typed key: context.WithValue matches on both the dynamic type and the value,
// so a bare string on either side would make the handler silently fail to find
// the channel. The handler itself is defense in depth, not the primary teardown
// path: the relay/helper call site recovers its own panics (see
// relay/helper/stream_scanner.go), so gopool only sees one if that recover is
// removed.
func TestRelayStopChanContextKeyIsShared(t *testing.T) {
	stopChan := make(chan bool, 1)
	ctx := context.WithValue(context.Background(), constant.ContextKeyRelayStopChan, stopChan)

	// Reproduce the read performed by the panic handler in gopool.go.
	got, ok := ctx.Value(constant.ContextKeyRelayStopChan).(chan bool)
	if !ok {
		t.Fatal("stop channel not retrievable through the typed context key")
	}
	SafeSendBool(got, true)

	select {
	case v := <-stopChan:
		if !v {
			t.Fatalf("stop signal = %v, want true", v)
		}
	case <-time.After(time.Second):
		t.Fatal("no stop signal delivered")
	}

	// A bare string key must not resolve to the same value; that collision is
	// what the typed key prevents.
	if ctx.Value("relay_stop_chan") != nil {
		t.Fatal("bare string key unexpectedly resolves to the relay stop channel")
	}
}

// Exercises the panic-handler wiring directly. The closure below has no
// recover of its own, so the panic escapes to gopool and the handler runs.
// This pins the handler's contract; it does not exercise a live production
// path, because relay goroutines recover their own panics before gopool sees
// them.
func TestRelayPoolPanicHandlerSignalsStopChan(t *testing.T) {
	stopChan := make(chan bool, 1)
	ctx := context.WithValue(context.Background(), constant.ContextKeyRelayStopChan, stopChan)
	RelayCtxGo(ctx, func() {
		panic("relay goroutine panic")
	})

	select {
	case v := <-stopChan:
		if !v {
			t.Fatalf("stop signal = %v, want true", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("panic in a relay goroutine did not signal the stop channel")
	}
}
