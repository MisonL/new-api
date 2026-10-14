package model

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// GetPricing/GetVendors/GetSupportedEndpointMap read cache state that
// updatePricing replaces wholesale. Reading a map while it is being written is
// a fatal runtime error (not a recoverable panic), so the readers must be
// serialized against the writer. Run with -race.
func TestPricingCacheReadersRaceWithInvalidate(t *testing.T) {
	// Without a database updatePricing returns at its first step, and with no
	// abilities the maps it publishes are empty -- either way the readers below
	// would never touch a populated map and this test would prove nothing. The
	// fixture installs an in-memory database with one routable ability.
	db := setupPricingEndpointTypesTestDB(t)
	require.NoError(t, db.Create(&Channel{Id: 7201, Name: "race", Key: "sk-r", Type: 1, Status: 1, Group: "default"}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "race-model", ChannelId: 7201, Enabled: true}).Error)

	// Force the stale path so GetPricing actually rebuilds under the write lock.
	updatePricingLock.Lock()
	lastGetPricingTime = time.Time{}
	updatePricingLock.Unlock()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Concurrent readers, each of which may trigger a refresh.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = GetPricing()
				_ = GetVendors()
				_ = GetSupportedEndpointMap()
				// The group-scoped lookup reads the same snapshot, plus a second
				// map published alongside it. Both must be covered here.
				_ = GetModelSupportEndpointTypesForGroups("race-model", []string{"default"})
				_ = GetModelSupportEndpointTypes("race-model")
			}
		}()
	}

	// Concurrent invalidator, standing in for a settings change. It keeps
	// invalidating for as long as the readers run, so the readers really do
	// race against repeated republication of the maps rather than against a
	// burst that ends before they start.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			InvalidatePricingCache()
			time.Sleep(time.Millisecond)
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}
