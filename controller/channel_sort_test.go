package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

type channelListResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Items      []model.Channel   `json:"items"`
		Total      int               `json:"total"`
		TypeCounts map[string]uint64 `json:"type_counts"`
	} `json:"data"`
	Message string `json:"message"`
}

type channelPriorityListResponse struct {
	Success bool                    `json:"success"`
	Data    []model.ChannelPriority `json:"data"`
	Message string                  `json:"message"`
}

type channelPinOrderListResponse struct {
	Success bool                    `json:"success"`
	Data    []model.ChannelPinOrder `json:"data"`
	Message string                  `json:"message"`
}

func TestGetAllChannelsAppliesServerSideSortQuery(t *testing.T) {
	setupChannelControllerTestDB(t)
	channels := []*model.Channel{
		{Id: 1, Name: "beta", Key: "sk-beta", Models: "gpt-5", Group: "default", Priority: common.GetPointer[int64](30)},
		{Id: 2, Name: "alpha", Key: "sk-alpha", Models: "gpt-5", Group: "default", Priority: common.GetPointer[int64](10)},
		{Id: 3, Name: "gamma", Key: "sk-gamma", Models: "gpt-5", Group: "default", Priority: common.GetPointer[int64](20)},
	}
	for _, channel := range channels {
		require.NoError(t, model.DB.Create(channel).Error)
	}

	ctx, recorder := newChannelControllerContext(t, http.MethodGet, "/api/channel/?p=1&page_size=10&sort_by=name&sort_order=asc", nil)

	GetAllChannels(ctx)

	var response channelListResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Len(t, response.Data.Items, 3)
	require.Equal(t, []int{2, 1, 3}, []int{response.Data.Items[0].Id, response.Data.Items[1].Id, response.Data.Items[2].Id})
}

func TestSearchChannelsAppliesServerSideSortQuery(t *testing.T) {
	setupChannelControllerTestDB(t)
	channels := []*model.Channel{
		{Id: 1, Name: "beta", Key: "sk-beta", Models: "gpt-5", Group: "default", Priority: common.GetPointer[int64](30), ResponseTime: 300},
		{Id: 2, Name: "alpha", Key: "sk-alpha", Models: "gpt-5", Group: "default", Priority: common.GetPointer[int64](10), ResponseTime: 100},
		{Id: 3, Name: "gamma", Key: "sk-gamma", Models: "gpt-5", Group: "default", Priority: common.GetPointer[int64](20), ResponseTime: 200},
	}
	for _, channel := range channels {
		require.NoError(t, model.DB.Create(channel).Error)
	}

	ctx, recorder := newChannelControllerContext(t, http.MethodGet, "/api/channel/search?keyword=a&model=gpt&p=1&page_size=10&sort_by=response_time&sort_order=asc", nil)

	SearchChannels(ctx)

	var response channelListResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Len(t, response.Data.Items, 3)
	require.Equal(t, []int{2, 3, 1}, []int{response.Data.Items[0].Id, response.Data.Items[1].Id, response.Data.Items[2].Id})
}

func TestGetTopChannelPrioritiesReturnsPinnedOrder(t *testing.T) {
	setupChannelControllerTestDB(t)
	channels := []*model.Channel{
		{Id: 1, Name: "zero", Key: "sk-zero", Models: "gpt-5", Group: "default", Priority: common.GetPointer[int64](0)},
		{Id: 2, Name: "low", Key: "sk-low", Models: "gpt-5", Group: "default", Priority: common.GetPointer[int64](10)},
		{Id: 3, Name: "high", Key: "sk-high", Models: "gpt-5", Group: "default", Priority: common.GetPointer[int64](20)},
	}
	for _, channel := range channels {
		require.NoError(t, model.DB.Create(channel).Error)
	}

	ctx, recorder := newChannelControllerContext(t, http.MethodGet, "/api/channel/priorities", nil)

	GetTopChannelPriorities(ctx)

	var response channelPriorityListResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Equal(t, []model.ChannelPriority{
		{Id: 3, Priority: common.GetPointer[int64](20)},
		{Id: 2, Priority: common.GetPointer[int64](10)},
	}, response.Data)
}

func TestGetTopChannelPinOrdersReturnsIndependentPinnedOrder(t *testing.T) {
	setupChannelControllerTestDB(t)
	priorityOne := int64(100)
	priorityTwo := int64(1)
	channels := []*model.Channel{
		{Id: 1, Name: "route-high", Key: "sk-one", Models: "gpt-5", Group: "default", Priority: &priorityOne, PinOrder: common.GetPointer[int64](10)},
		{Id: 2, Name: "route-low", Key: "sk-two", Models: "gpt-5", Group: "default", Priority: &priorityTwo, PinOrder: common.GetPointer[int64](20)},
		{Id: 3, Name: "unpinned", Key: "sk-three", Models: "gpt-5", Group: "default", Priority: &priorityOne, PinOrder: common.GetPointer[int64](0)},
	}
	for _, channel := range channels {
		require.NoError(t, model.DB.Create(channel).Error)
	}

	ctx, recorder := newChannelControllerContext(t, http.MethodGet, "/api/channel/pins", nil)
	GetTopChannelPinOrders(ctx)

	var response channelPinOrderListResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Equal(t, []model.ChannelPinOrder{
		{Id: 2, PinOrder: common.GetPointer[int64](20)},
		{Id: 1, PinOrder: common.GetPointer[int64](10)},
	}, response.Data)
}

func TestUpdateChannelPinOrdersDoesNotChangeRoutePriority(t *testing.T) {
	setupChannelControllerTestDB(t)
	priorityOne := int64(100)
	priorityTwo := int64(1)
	for _, channel := range []*model.Channel{
		{Id: 1, Name: "route-high", Key: "sk-one", Models: "gpt-5", Group: "default", Priority: &priorityOne},
		{Id: 2, Name: "route-low", Key: "sk-two", Models: "gpt-5", Group: "default", Priority: &priorityTwo},
	} {
		require.NoError(t, model.DB.Create(channel).Error)
	}

	ctx, recorder := newChannelControllerContext(t, http.MethodPut, "/api/channel/pins", []map[string]any{
		{"id": 1, "pin_order": 1000},
		{"id": 2, "pin_order": 2000},
	})
	UpdateChannelPinOrders(ctx)

	var response channelPriorityBatchResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Equal(t, 2, response.Data)

	updatedOne, err := model.GetChannelById(1, false)
	require.NoError(t, err)
	require.Equal(t, priorityOne, updatedOne.GetPriority())
	require.Equal(t, int64(1000), updatedOne.GetPinOrder())
	updatedTwo, err := model.GetChannelById(2, false)
	require.NoError(t, err)
	require.Equal(t, priorityTwo, updatedTwo.GetPriority())
	require.Equal(t, int64(2000), updatedTwo.GetPinOrder())
}

func TestGetAllChannelsAppliesGroupFilterQuery(t *testing.T) {
	setupChannelControllerTestDB(t)
	channels := []*model.Channel{
		{Id: 1, Name: "default", Key: "sk-default", Models: "gpt-5", Group: "default", Type: 1, Priority: common.GetPointer[int64](30)},
		{Id: 2, Name: "paid", Key: "sk-paid", Models: "gpt-5", Group: "default,paid", Type: 2, Priority: common.GetPointer[int64](20)},
		{Id: 3, Name: "paid-disabled", Key: "sk-disabled", Models: "gpt-5", Group: "paid", Type: 2, Status: common.ChannelStatusManuallyDisabled, Priority: common.GetPointer[int64](10)},
	}
	for _, channel := range channels {
		require.NoError(t, model.DB.Create(channel).Error)
	}

	ctx, recorder := newChannelControllerContext(t, http.MethodGet, "/api/channel/?p=1&page_size=10&group=paid&status=enabled", nil)

	GetAllChannels(ctx)

	var response channelListResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Equal(t, 1, response.Data.Total)
	require.Len(t, response.Data.Items, 1)
	require.Equal(t, 2, response.Data.Items[0].Id)
	require.Equal(t, map[string]uint64{"2": 1}, response.Data.TypeCounts)
}

func TestGetAllChannelsTagModeAppliesGroupFilterQuery(t *testing.T) {
	setupChannelControllerTestDB(t)
	alpha := "alpha"
	beta := "beta"
	channels := []*model.Channel{
		{Id: 1, Name: "default-alpha", Key: "sk-default", Models: "gpt-5", Group: "default", Tag: &alpha, Priority: common.GetPointer[int64](30)},
		{Id: 2, Name: "paid-alpha", Key: "sk-paid-alpha", Models: "gpt-5", Group: "paid", Tag: &alpha, Priority: common.GetPointer[int64](20)},
		{Id: 3, Name: "paid-beta", Key: "sk-paid-beta", Models: "gpt-5", Group: "paid", Tag: &beta, Priority: common.GetPointer[int64](10)},
	}
	for _, channel := range channels {
		require.NoError(t, model.DB.Create(channel).Error)
	}

	ctx, recorder := newChannelControllerContext(t, http.MethodGet, "/api/channel/?p=1&page_size=10&tag_mode=true&group=paid", nil)

	GetAllChannels(ctx)

	var response channelListResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Equal(t, 2, response.Data.Total)
	require.Len(t, response.Data.Items, 2)
	require.Equal(t, []int{2, 3}, []int{response.Data.Items[0].Id, response.Data.Items[1].Id})
}

func TestSearchChannelsTagModePreservesSearchFilters(t *testing.T) {
	setupChannelControllerTestDB(t)
	alpha := "alpha"
	channels := []*model.Channel{
		{Id: 1, Name: "paid-alpha-match", Key: "sk-match", Models: "gpt-5", Group: "paid", Tag: &alpha, Priority: common.GetPointer[int64](30)},
		{Id: 2, Name: "paid-alpha-other", Key: "sk-other", Models: "gpt-5", Group: "paid", Tag: &alpha, Priority: common.GetPointer[int64](20)},
		{Id: 3, Name: "free-alpha-match", Key: "sk-free", Models: "gpt-5", Group: "free", Tag: &alpha, Priority: common.GetPointer[int64](10)},
		{Id: 4, Name: "paid-alpha-match-gpt4", Key: "sk-gpt4", Models: "gpt-4", Group: "paid", Tag: &alpha, Priority: common.GetPointer[int64](5)},
	}
	for _, channel := range channels {
		require.NoError(t, model.DB.Create(channel).Error)
	}

	ctx, recorder := newChannelControllerContext(t, http.MethodGet, "/api/channel/search?tag_mode=true&keyword=match&group=paid&model=gpt-5&p=1&page_size=10", nil)

	SearchChannels(ctx)

	var response channelListResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Equal(t, 1, response.Data.Total)
	require.Len(t, response.Data.Items, 1)
	require.Equal(t, 1, response.Data.Items[0].Id)
}
