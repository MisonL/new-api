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
