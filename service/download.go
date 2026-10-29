package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// WorkerRequest Worker请求的数据结构
type WorkerRequest struct {
	URL     string            `json:"url"`
	Key     string            `json:"key"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// DoWorkerRequest 通过Worker发送请求
func DoWorkerRequest(req *WorkerRequest) (*http.Response, error) {
	if !system_setting.EnableWorker() {
		return nil, fmt.Errorf("worker not enabled")
	}
	if !system_setting.WorkerAllowHttpImageRequestEnabled && !strings.HasPrefix(req.URL, "https") {
		return nil, fmt.Errorf("only support https url")
	}

	// SSRF防护：验证请求URL
	fetchSetting := system_setting.GetFetchSettingSnapshot()
	if err := common.ValidateURLWithFetchSetting(req.URL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, fmt.Errorf("request reject: %v", err)
	}

	workerUrl := system_setting.WorkerUrl
	if !strings.HasSuffix(workerUrl, "/") {
		workerUrl += "/"
	}

	// 序列化worker请求数据
	workerPayload, err := common.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal worker payload: %v", err)
	}

	return GetHttpClient().Post(workerUrl, "application/json", bytes.NewBuffer(workerPayload))
}

// defaultDownloadTimeout 是未配置 RELAY_TIMEOUT 时的下载上限。
const defaultDownloadTimeout = 5 * time.Minute

// downloadTimeout 取 relay 超时；未配置时回退到有限上限，避免无限期挂起。
func downloadTimeout() time.Duration {
	if common.RelayTimeout <= 0 {
		return defaultDownloadTimeout
	}
	return time.Duration(common.RelayTimeout) * time.Second
}

// cancelOnCloseBody 把请求 context 的 cancel 绑定到响应体关闭，保证超时
// 覆盖整个读取过程，而不会在 DoDownloadRequest 返回时就把读取掐断。
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

func DoDownloadRequest(originUrl string, reason ...string) (resp *http.Response, err error) {
	if system_setting.EnableWorker() {
		common.SysLog(fmt.Sprintf("downloading file from worker: %s, reason: %s", common.MaskSensitiveInfo(originUrl), strings.Join(reason, ", ")))
		req := &WorkerRequest{
			URL: originUrl,
			Key: system_setting.WorkerValidKey,
		}
		return DoWorkerRequest(req)
	} else {
		common.SysLog(fmt.Sprintf("downloading from origin: %s, reason: %s", common.MaskSensitiveInfo(originUrl), strings.Join(reason, ", ")))
		fetchSetting := system_setting.GetFetchSettingSnapshot()
		if err := common.ValidateURLWithFetchSetting(originUrl, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
			return nil, fmt.Errorf("invalid request URL: %v", err)
		}
		// context.Background() 没有截止时间：上游挂住时下载会一直占着连接与
		// goroutine。超时 context 必须在响应体读完后才 cancel，否则一旦本函数
		// 返回就会掐断调用方的读取，因此把 cancel 挂到 body 的 Close 上。
		requestContext, cancel := context.WithTimeout(context.Background(), downloadTimeout())
		request, err := http.NewRequestWithContext(common.WithSSRFValidation(requestContext), http.MethodGet, originUrl, nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("request reject: %v", err)
		}
		resp, err := GetHttpClient().Do(request)
		if err != nil {
			cancel()
			return nil, err
		}
		if resp.Body != nil {
			resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
		} else {
			cancel()
		}
		return resp, nil
	}
}
