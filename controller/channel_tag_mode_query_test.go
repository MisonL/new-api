package controller

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 注册一个只计数的 gorm 查询回调，用于断言 tag 模式的查询数量。
func countQueries(t *testing.T, db *gorm.DB) *int64 {
	t.Helper()
	var counter int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(
		"test_count_queries_"+t.Name(), func(tx *gorm.DB) {
			atomic.AddInt64(&counter, 1)
		}))
	return &counter
}

// tag 模式此前按分页返回的每个标签单独发一次查询：page_size 个标签就是
// page_size 次往返，标签越多管理后台列表越慢，数据库压力随标签数线性放大。
// 这里要求同页所有标签的渠道一次性查回，再在内存里按标签聚合。
func TestGetAllChannelsTagModeUsesSingleChannelQuery(t *testing.T) {
	setupChannelControllerTestDB(t)

	alpha, beta, gamma := "alpha", "beta", "gamma"
	channels := []*model.Channel{
		{Id: 1, Name: "alpha-1", Key: "sk-a1", Models: "gpt-5", Group: "default", Tag: &alpha, Priority: common.GetPointer[int64](30)},
		{Id: 2, Name: "alpha-2", Key: "sk-a2", Models: "gpt-5", Group: "default", Tag: &alpha, Priority: common.GetPointer[int64](10)},
		{Id: 3, Name: "beta-1", Key: "sk-b1", Models: "gpt-5", Group: "default", Tag: &beta, Priority: common.GetPointer[int64](20)},
		{Id: 4, Name: "gamma-1", Key: "sk-g1", Models: "gpt-5", Group: "default", Tag: &gamma, Priority: common.GetPointer[int64](5)},
	}
	for _, channel := range channels {
		require.NoError(t, model.DB.Create(channel).Error)
	}

	counter := countQueries(t, model.DB)
	ctx, recorder := newChannelControllerContext(t, http.MethodGet, "/api/channel/?p=1&page_size=10&tag_mode=true", nil)
	GetAllChannels(ctx)

	var response channelListResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	// tag 模式下 total 是标签数，items 是这些标签下的全部渠道。
	require.Equal(t, 3, response.Data.Total)
	require.Len(t, response.Data.Items, 4)
	// 返回顺序必须仍然按标签分组，组内按排序键。
	require.Equal(t, []int{1, 2, 3, 4}, []int{
		response.Data.Items[0].Id, response.Data.Items[1].Id,
		response.Data.Items[2].Id, response.Data.Items[3].Id,
	})

	// 固定开销 3 次查询（标签页、标签计数、type_counts 聚合）之外，渠道
	// 读取只能有 1 次；逐标签查询时会变成 3+标签数 次。
	require.LessOrEqual(t, atomic.LoadInt64(counter), int64(4),
		"tag 模式的渠道读取必须是单次查询：实际 %d 次", atomic.LoadInt64(counter))
}

func TestSearchChannelsTagModeUsesSingleChannelQuery(t *testing.T) {
	setupChannelControllerTestDB(t)

	alpha, beta, gamma := "alpha", "beta", "gamma"
	channels := []*model.Channel{
		{Id: 1, Name: "paid-alpha-match", Key: "sk-1", Models: "gpt-5", Group: "paid", Tag: &alpha, Priority: common.GetPointer[int64](30)},
		{Id: 2, Name: "paid-alpha-match-2", Key: "sk-2", Models: "gpt-5", Group: "paid", Tag: &alpha, Priority: common.GetPointer[int64](20)},
		{Id: 3, Name: "paid-beta-match", Key: "sk-3", Models: "gpt-5", Group: "paid", Tag: &beta, Priority: common.GetPointer[int64](15)},
		{Id: 4, Name: "paid-gamma-match", Key: "sk-4", Models: "gpt-5", Group: "paid", Tag: &gamma, Priority: common.GetPointer[int64](10)},
	}
	for _, channel := range channels {
		require.NoError(t, model.DB.Create(channel).Error)
	}

	counter := countQueries(t, model.DB)
	ctx, recorder := newChannelControllerContext(t, http.MethodGet,
		"/api/channel/search?tag_mode=true&keyword=match&group=paid&model=gpt-5&p=1&page_size=10", nil)
	SearchChannels(ctx)

	var response channelListResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	require.Equal(t, 4, response.Data.Total)
	require.Len(t, response.Data.Items, 4)

	// SearchTags 本身是"子查询 + DISTINCT 包装"两次执行，加上渠道读取 1 次；
	// 逐标签查询时是 2+标签数 次。
	require.LessOrEqual(t, atomic.LoadInt64(counter), int64(3),
		"搜索 tag 模式的渠道读取必须是单次查询：实际 %d 次", atomic.LoadInt64(counter))
}
