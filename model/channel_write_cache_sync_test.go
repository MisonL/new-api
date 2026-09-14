package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestDirectChannelWritesRefreshRouteCacheAfterCommit(t *testing.T) {
	prepareChannelCacheTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	priority := int64(10)
	weight := uint(1)
	initialMapping := `{"client-model":"provider-before"}`
	channel := &Channel{
		Id:           90801,
		Name:         "direct-channel-write-cache-sync",
		Key:          "test-key",
		Status:       common.ChannelStatusEnabled,
		Models:       "client-model",
		Group:        "default",
		Priority:     &priority,
		Weight:       &weight,
		ModelMapping: &initialMapping,
	}

	require.NoError(t, InsertChannel(channel))
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, initialMapping, cached.GetModelMapping())

	updatedMapping := `{"client-model":"provider-after"}`
	channel.ModelMapping = &updatedMapping
	require.NoError(t, channel.Update())
	cached, err = CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, updatedMapping, cached.GetModelMapping())

	require.NoError(t, channel.Delete())
	_, err = CacheGetChannel(channel.Id)
	require.Error(t, err)
}

func TestDirectBatchDeleteChannelsRefreshesRouteCacheAfterCommit(t *testing.T) {
	prepareChannelCacheTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	channels := []*Channel{
		{Id: 90802, Name: "direct-batch-delete-a", Key: "key-a", Status: common.ChannelStatusEnabled, Models: "batch-model-a", Group: "default"},
		{Id: 90803, Name: "direct-batch-delete-b", Key: "key-b", Status: common.ChannelStatusEnabled, Models: "batch-model-b", Group: "default"},
	}
	for _, channel := range channels {
		require.NoError(t, InsertChannel(channel))
		_, err := CacheGetChannel(channel.Id)
		require.NoError(t, err)
	}

	require.NoError(t, BatchDeleteChannels([]int{channels[0].Id, channels[1].Id}))
	for _, channel := range channels {
		_, err := CacheGetChannel(channel.Id)
		require.Error(t, err)
	}
}

func TestBatchInsertSaveAndBatchTagWritesRefreshRouteCache(t *testing.T) {
	prepareChannelCacheTest(t)
	previousMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCacheEnabled })

	initialMapping := `{"cache-write-model":"provider-before"}`
	channel := Channel{
		Id:           90804,
		Name:         "batch-save-tag-cache-sync",
		Key:          "batch-save-tag-key",
		Status:       common.ChannelStatusEnabled,
		Models:       "cache-write-model",
		Group:        "default",
		ModelMapping: &initialMapping,
	}
	require.NoError(t, BatchInsertChannels([]Channel{channel}))
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, initialMapping, cached.GetModelMapping())

	updatedMapping := `{"cache-write-model":"provider-after"}`
	channel.ModelMapping = &updatedMapping
	require.NoError(t, channel.SaveWithoutKey())
	cached, err = CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, updatedMapping, cached.GetModelMapping())

	tag := "cache-write-tag"
	require.NoError(t, BatchSetChannelTag([]int{channel.Id}, &tag))
	cached, err = CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, tag, cached.GetTag())
}
