package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

// failOnSeekBodyStorage lets request validation consume a reusable body while
// making the later per-channel body preparation fail. This covers the local
// failure boundary between selecting a channel and sending an upstream call.
type failOnSeekBodyStorage struct {
	common.BodyStorage
	seekCount int
	failAfter int
}

func (s *failOnSeekBodyStorage) Seek(offset int64, whence int) (int64, error) {
	s.seekCount++
	if s.seekCount > s.failAfter {
		return 0, errors.New("body storage seek failed for test")
	}
	return s.BodyStorage.Seek(offset, whence)
}

func TestRelayBodyPreparationFailureDoesNotRecordUsedChannel(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	configureResponsesReadBodyRetryRelay(t)
	first, _ := seedResponsesReadBodyRetryChannels(t, db, "http://127.0.0.1:9", "http://127.0.0.1:10")

	ctx, recorder := newResponsesReadBodyRetryContext(t, first, context.Background(), visibleResponsesReadBodyRetryRequest())
	storage, err := common.CreateBodyStorage([]byte(visibleResponsesReadBodyRetryRequest()))
	require.NoError(t, err)
	ctx.Set(common.KeyBodyStorage, &failOnSeekBodyStorage{BodyStorage: storage, failAfter: 2})
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })

	Relay(ctx, types.RelayFormatOpenAIResponses)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Empty(t, ctx.GetStringSlice("use_channel"))
}
