package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/console_setting"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

const (
	requestTimeout   = 30 * time.Second
	httpTimeout      = 10 * time.Second
	uptimeKeySuffix  = "_24"
	apiStatusPath    = "/api/status-page/"
	apiHeartbeatPath = "/api/status-page/heartbeat/"
)

type Monitor struct {
	Name   string  `json:"name"`
	Uptime float64 `json:"uptime"`
	Status int     `json:"status"`
	Group  string  `json:"group,omitempty"`
}

type UptimeGroupResult struct {
	CategoryName string    `json:"categoryName"`
	Monitors     []Monitor `json:"monitors"`
}

func getAndDecode(ctx context.Context, client *http.Client, url string, dest interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New("non-200 status")
	}

	return common.DecodeJson(resp.Body, dest)
}

func fetchGroupData(ctx context.Context, client *http.Client, groupConfig map[string]interface{}) UptimeGroupResult {
	url, _ := groupConfig["url"].(string)
	slug, _ := groupConfig["slug"].(string)
	categoryName, _ := groupConfig["categoryName"].(string)

	result := UptimeGroupResult{
		CategoryName: categoryName,
		Monitors:     []Monitor{},
	}

	if url == "" || slug == "" {
		return result
	}

	baseURL := strings.TrimSuffix(url, "/")

	var statusData struct {
		PublicGroupList []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			MonitorList []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"monitorList"`
		} `json:"publicGroupList"`
	}

	var heartbeatData struct {
		HeartbeatList map[string][]struct {
			Status int `json:"status"`
		} `json:"heartbeatList"`
		UptimeList map[string]float64 `json:"uptimeList"`
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return getAndDecode(gCtx, client, baseURL+apiStatusPath+slug, &statusData)
	})
	g.Go(func() error {
		return getAndDecode(gCtx, client, baseURL+apiHeartbeatPath+slug, &heartbeatData)
	})

	if g.Wait() != nil {
		return result
	}

	for _, pg := range statusData.PublicGroupList {
		if len(pg.MonitorList) == 0 {
			continue
		}

		for _, m := range pg.MonitorList {
			monitor := Monitor{
				Name:  m.Name,
				Group: pg.Name,
			}

			monitorID := strconv.Itoa(m.ID)

			if uptime, exists := heartbeatData.UptimeList[monitorID+uptimeKeySuffix]; exists {
				monitor.Uptime = uptime
			}

			if heartbeats, exists := heartbeatData.HeartbeatList[monitorID]; exists && len(heartbeats) > 0 {
				monitor.Status = heartbeats[0].Status
			}

			result.Monitors = append(result.Monitors, monitor)
		}
	}

	return result
}

// uptimeCacheTTL 是 uptime 状态页结果的缓存时间。该接口是匿名路由，未缓存时
// 每次请求都会按配置的每个分组发起两次出站请求，匿名用户可以把它放大成对
// 上游状态页的持续压力。
const uptimeCacheTTL = 30 * time.Second

// uptimeStatusCache 缓存 uptime 状态页结果，并用 singleflight 让并发请求
// 共用同一次抓取。key 由分组配置生成，管理端改配置后会立即换 key。
type uptimeStatusCache struct {
	mu        sync.Mutex
	key       string
	expiresAt time.Time
	results   []UptimeGroupResult
	flight    singleflight.Group
	ttl       time.Duration
	now       func() time.Time
}

func (c *uptimeStatusCache) Get(key string, fetch func() []UptimeGroupResult) []UptimeGroupResult {
	c.mu.Lock()
	if c.key == key && c.now().Before(c.expiresAt) {
		results := c.results
		c.mu.Unlock()
		return results
	}
	c.mu.Unlock()

	value, _, _ := c.flight.Do(key, func() (interface{}, error) {
		results := fetch()
		c.mu.Lock()
		c.key = key
		c.expiresAt = c.now().Add(c.ttl)
		c.results = results
		c.mu.Unlock()
		return results, nil
	})
	results, _ := value.([]UptimeGroupResult)
	return results
}

var uptimeStatus = &uptimeStatusCache{ttl: uptimeCacheTTL, now: time.Now}

func uptimeGroupsKey(groups []map[string]interface{}) string {
	return common.GetJsonString(groups)
}

// fetchUptimeGroups 抓取全部分组。抓取结果被缓存、可能被多个请求共用，
// 因此不能绑定到触发它的那个请求的 context：任一客户端断开都不应取消
// 其他人正在等待的结果。整体仍受 requestTimeout 约束。
func fetchUptimeGroups(groups []map[string]interface{}) []UptimeGroupResult {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	client := &http.Client{Timeout: httpTimeout}
	results := make([]UptimeGroupResult, len(groups))

	g, gCtx := errgroup.WithContext(ctx)
	for i, group := range groups {
		i, group := i, group
		g.Go(func() error {
			results[i] = fetchGroupData(gCtx, client, group)
			return nil
		})
	}

	g.Wait()
	return results
}

func GetUptimeKumaStatus(c *gin.Context) {
	groups := console_setting.GetUptimeKumaGroups()
	if len(groups) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": []UptimeGroupResult{}})
		return
	}

	results := uptimeStatus.Get(uptimeGroupsKey(groups), func() []UptimeGroupResult {
		return fetchUptimeGroups(groups)
	})

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": results})
}
