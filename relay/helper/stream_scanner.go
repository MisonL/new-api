package helper

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"

	"github.com/gin-gonic/gin"
)

const (
	InitialScannerBufferSize    = 64 << 10 // 64KB (64*1024)
	DefaultMaxScannerBufferSize = 64 << 20 // 64MB (64*1024*1024) default SSE buffer size
	DefaultStreamingTimeout     = 300 * time.Second
	DefaultPingInterval         = 10 * time.Second

	// streamShutdownWriteGrace 是扫描器返回前等待阻塞写入失败的宽限期。
	streamShutdownWriteGrace = 5 * time.Second
)

// responseWriterUnwrapper exposes gin's underlying http.ResponseWriter to
// http.ResponseController; gin's ResponseWriter implements neither
// SetWriteDeadline nor Unwrap.
type responseWriterUnwrapper struct{ gin.ResponseWriter }

func (w responseWriterUnwrapper) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func getScannerBufferSize() int {
	if constant.StreamScannerMaxBufferMB > 0 {
		return constant.StreamScannerMaxBufferMB << 20
	}
	return DefaultMaxScannerBufferSize
}

// GetScannerBufferSize exposes the shared scanner's line ceiling to the channels
// that drive their own bufio.Scanner. A bare bufio.NewScanner caps a token at
// bufio.MaxScanTokenSize (64KiB), so one upstream SSE frame larger than that --
// a big tool-call payload is the usual case -- makes Scan stop with ErrTooLong
// and silently truncates the stream before its terminal marker. Those channels
// must use the same ceiling, and the same StreamScannerMaxBufferMB override, as
// the shared scanner; otherwise the override only applies to one code path.
func GetScannerBufferSize() int {
	return getScannerBufferSize()
}

func copyCodexResponsesHeaders(c *gin.Context, resp *http.Response) {
	if c == nil || c.Writer == nil || resp == nil {
		return
	}
	for _, name := range []string{"X-Reasoning-Included", "X-Codex-Turn-State"} {
		values := resp.Header.Values(name)
		if !service.ShouldCopyUpstreamHeader(c, name, values) {
			continue
		}
		for _, value := range values {
			if value != "" {
				c.Writer.Header().Add(name, value)
			}
		}
	}
}

// CopyCodexResponsesHeaders exposes the small allowlist of response headers
// that Codex clients use for Responses turn state. It is also used by the
// Codex folded SSE executor, which owns its own raw stream reader.
func CopyCodexResponsesHeaders(c *gin.Context, resp *http.Response) {
	copyCodexResponsesHeaders(c, resp)
}

func handleScannerError(c *gin.Context, info *relaycommon.RelayInfo, err error) {
	if err == nil || err == io.EOF {
		return
	}
	if info.StreamStatus.EndReason == relaycommon.StreamEndReasonFirstDataTimeout {
		return
	}
	if info.StreamStatus.IsNormalEnd() {
		logger.LogInfo(c, "scanner closed after normal stream end: "+err.Error())
		return
	}
	if info.StreamStatus.IsCanceled() {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		logger.LogInfo(c, "scanner closed after client disconnect: "+err.Error())
		return
	}
	if types.IsUpstreamTransportInterruptedError(err) {
		logger.LogError(c, "upstream stream interrupted: "+err.Error())
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, err)
		return
	}
	if relaycommon.IsBenignDisconnectErrorMessage(err.Error()) {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		logger.LogInfo(c, "scanner closed after client disconnect: "+err.Error())
		return
	}
	logger.LogError(c, "scanner error: "+err.Error())
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
}

type StreamScannerOptions struct {
	FirstDataEventTimeout time.Duration
}

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *StreamResult)) {
	StreamScannerHandlerWithOptions(c, resp, info, dataHandler, StreamScannerOptions{})
}

func StreamScannerHandlerWithOptions(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *StreamResult), options StreamScannerOptions) {

	if resp == nil || dataHandler == nil {
		return
	}

	// 保留预置的 StreamStatus（例如上游已记录的错误），仅在为空时初始化
	if info.StreamStatus == nil {
		info.StreamStatus = relaycommon.NewStreamStatus()
	}
	if info.ResponsesStreamObservation == nil {
		info.ResponsesStreamObservation = relaycommon.NewResponsesStreamObservation()
	}

	streamingTimeout := time.Duration(constant.StreamingTimeout) * time.Second
	if streamingTimeout <= 0 {
		streamingTimeout = DefaultStreamingTimeout
	}
	firstDataEventTimeout := options.FirstDataEventTimeout
	var firstDataTimer *time.Timer
	var firstDataTimeout <-chan time.Time
	if firstDataEventTimeout > 0 {
		firstDataTimer = time.NewTimer(firstDataEventTimeout)
		firstDataTimeout = firstDataTimer.C
	}

	var (
		stopChan              = make(chan bool, 3) // 增加缓冲区避免阻塞
		scanner               = bufio.NewScanner(resp.Body)
		ticker                = time.NewTicker(streamingTimeout)
		pingTicker            *time.Ticker
		writeMutex            sync.Mutex // Mutex to protect concurrent writes
		wg                    sync.WaitGroup
		receivedResponseCount atomic.Int64
		receivedDone          atomic.Bool
		firstDataReceived     atomic.Bool
	)
	notifyStop := func() {
		select {
		case stopChan <- true:
		default:
		}
	}

	generalSettings := operation_setting.GetGeneralSettingSnapshot()
	pingEnabled := generalSettings.PingIntervalEnabled && !info.DisablePing
	pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
	if pingInterval <= 0 {
		pingInterval = DefaultPingInterval
	}

	if pingEnabled {
		pingTicker = time.NewTicker(pingInterval)
	}

	if common.DebugEnabled {
		// print timeout and ping interval for debugging
		println("relay timeout seconds:", common.RelayTimeout)
		println("relay max idle conns:", common.RelayMaxIdleConns)
		println("relay max idle conns per host:", common.RelayMaxIdleConnsPerHost)
		println("streaming timeout seconds:", int64(streamingTimeout.Seconds()))
		println("ping interval seconds:", int64(pingInterval.Seconds()))
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, constant.ContextKeyRelayStopChan, stopChan)

	// 改进资源清理，确保所有 goroutine 正确退出
	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}

		// 通知所有 goroutine 停止
		notifyStop()
		cancel()

		ticker.Stop()
		if pingTicker != nil {
			pingTicker.Stop()
		}
		if firstDataTimer != nil {
			firstDataTimer.Stop()
		}

		// 返回后不允许任何协程再写 c.Writer：gin 会把 Context 放回池里复用，
		// 遗留协程的写会落到另一个请求的响应上。给底层连接设一个写截止时间，
		// 让卡在客户端写入上的 data handler / ping 写协程在宽限期内失败退出，
		// 然后等它们全部结束再返回。此前的实现最多等 5 秒就返回，遗留协程
		// 仍在写响应。
		if c.Writer != nil {
			_ = http.NewResponseController(responseWriterUnwrapper{c.Writer}).
				SetWriteDeadline(time.Now().Add(streamShutdownWriteGrace))
		}
		wg.Wait()
		if c.Writer != nil {
			_ = http.NewResponseController(responseWriterUnwrapper{c.Writer}).
				SetWriteDeadline(time.Time{})
		}
		info.ReceivedResponseCount = int(receivedResponseCount.Load())
		if info.StreamStatus.EndReason == relaycommon.StreamEndReasonNone {
			if receivedDone.Load() {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
			} else {
				err := fmt.Errorf("stream disconnected before completion")
				logger.LogError(c, "upstream stream ended before completion")
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, err)
			}
		}
		if (info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors()) || info.StreamStatus.IsCanceled() {
			logger.LogInfo(c, fmt.Sprintf("stream ended: %s", info.StreamStatus.Summary()))
		} else {
			logger.LogError(c, fmt.Sprintf("stream ended: %s, received=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount))
		}
	}()

	scanner.Buffer(make([]byte, InitialScannerBufferSize), getScannerBufferSize())
	scanner.Split(bufio.ScanLines)
	copyCodexResponsesHeaders(c, resp)
	SetEventStreamHeaders(c)

	// Handle ping data sending with improved error handling
	if pingEnabled && pingTicker != nil {
		wg.Add(1)
		gopool.Go(func() {
			defer func() {
				wg.Done()
				if r := recover(); r != nil {
					logger.LogError(c, fmt.Sprintf("ping goroutine panic: %v", r))
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("ping panic: %v", r))
					notifyStop()
				}
				if common.DebugEnabled {
					println("ping goroutine exited")
				}
			}()

			// 添加超时保护，防止 goroutine 无限运行
			maxPingDuration := 30 * time.Minute // 最大 ping 持续时间
			pingTimeout := time.NewTimer(maxPingDuration)
			defer pingTimeout.Stop()

			for {
				select {
				case <-pingTicker.C:
					// 使用超时机制防止写操作阻塞。写协程计入 wg：超时放弃等待后
					// 它可能仍持有 writeMutex 并写 c.Writer，扫描器必须等它退出
					// 才能返回。计数在 ping 协程自己的 +1 尚未 Done 时增加，
					// 与 Wait 并发是合法的。
					done := make(chan error, 1)
					wg.Add(1)
					gopool.Go(func() {
						defer wg.Done()
						writeMutex.Lock()
						defer writeMutex.Unlock()
						done <- PingData(c)
					})

					select {
					case err := <-done:
						if err != nil {
							logger.LogError(c, "ping data error: "+err.Error())
							info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, err)
							return
						}
						if common.DebugEnabled {
							println("ping data sent")
						}
					case <-time.After(10 * time.Second):
						logger.LogError(c, "ping data send timeout")
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, fmt.Errorf("ping send timeout"))
						return
					case <-ctx.Done():
						return
					case <-stopChan:
						return
					}
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				case <-c.Request.Context().Done():
					// 监听客户端断开连接
					return
				case <-pingTimeout.C:
					logger.LogError(c, "ping goroutine max duration reached")
					return
				}
			}
		})
	}

	dataChan := make(chan string, 10)

	wg.Add(1)
	gopool.Go(func() {
		defer func() {
			wg.Done()
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("data handler goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("handler panic: %v", r))
			}
			notifyStop()
		}()
		sr := newStreamResult(info.StreamStatus)
		for data := range dataChan {
			sr.reset()
			// 处理器 panic 时必须仍然释放写锁：本协程的 recover 在外层 defer，
			// 而等锁的 ping 写协程计入 wg，锁不释放会让 wg.Wait() 永久挂起。
			// 处理器 panic 时必须仍然释放写锁：本协程的 recover 在外层 defer，
			// 而等锁的 ping 写协程计入 wg，锁不释放会让 wg.Wait() 永久挂起。
			func() {
				writeMutex.Lock()
				defer writeMutex.Unlock()
				dataHandler(data, sr)
			}()
			if sr.IsStopped() {
				return
			}
		}
	})

	// Scanner goroutine with improved error handling
	wg.Add(1)
	common.RelayCtxGo(ctx, func() {
		defer func() {
			close(dataChan)
			wg.Done()
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("scanner goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("scanner panic: %v", r))
			}
			notifyStop()
			if common.DebugEnabled {
				println("scanner goroutine exited")
			}
		}()

		for scanner.Scan() {
			// 检查是否需要停止
			select {
			case <-stopChan:
				return
			case <-ctx.Done():
				return
			case <-c.Request.Context().Done():
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
				return
			default:
			}

			ticker.Reset(streamingTimeout)
			data := scanner.Text()
			if common.DebugEnabled {
				println(data)
			}

			data = strings.TrimSpace(data)
			isDone := strings.HasPrefix(data, "[DONE]")
			if strings.HasPrefix(data, "data:") {
				data = strings.TrimSpace(data[len("data:"):])
				isDone = strings.HasPrefix(data, "[DONE]")
			} else if !isDone {
				continue
			}
			if data == "" {
				continue
			}
			if !isDone {
				firstDataReceived.Store(true)
				if firstDataTimer != nil {
					firstDataTimer.Stop()
				}
				info.SetFirstResponseTime()
				receivedResponseCount.Add(1)

				select {
				case dataChan <- data:
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				}
			} else {
				receivedDone.Store(true)
				if info.ResponsesStreamObservation != nil {
					info.ResponsesStreamObservation.MarkDone()
				}
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				if common.DebugEnabled {
					println("received [DONE], stopping scanner")
				}
				return
			}
		}

		handleScannerError(c, info, scanner.Err())
	})

	// 主循环等待完成或超时
	for {
		select {
		case <-firstDataTimeout:
			if !firstDataReceived.Load() {
				info.StreamStatus.SetEndReason(
					relaycommon.StreamEndReasonFirstDataTimeout,
					fmt.Errorf("stream did not receive a data event within %s", firstDataEventTimeout),
				)
				return
			}
			firstDataTimeout = nil
		case <-ticker.C:
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
			return
		case <-stopChan:
			// EndReason already set by the goroutine that triggered stopChan
			return
		case <-c.Request.Context().Done():
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
			return
		}
	}
}
