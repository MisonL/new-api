package common

import (
	"errors"
	"fmt"
	"io"
	"math"
)

// 上游响应体读取上限，分档与仓库既有封顶（claude/xai/responses 64 MiB、
// gemini native 128 MiB、模型列表 8 MiB）保持一致。上游返回超大响应时，
// 直接 io.ReadAll 会让每个并发请求各持一份完整响应体。
const (
	// UpstreamSmallResponseLimit 用于模型列表、状态查询等小 JSON 信封。
	UpstreamSmallResponseLimit int64 = 8 << 20
	// UpstreamJSONResponseLimit 用于非流式 JSON 响应。
	UpstreamJSONResponseLimit int64 = 64 << 20
	// UpstreamMediaResponseLimit 用于可能内联 base64 媒体或音频的响应。
	UpstreamMediaResponseLimit int64 = 128 << 20
)

// ReadAllWithLimit reads a response body with an explicit byte limit. An
// oversized body returns an error instead of the truncated prefix that
// io.LimitReader alone would silently produce, so callers never parse or
// persist a partial payload as if it were complete.
func ReadAllWithLimit(reader io.Reader, limit int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("response body reader is nil")
	}
	if limit <= 0 {
		return nil, errors.New("response body limit must be positive")
	}
	if limit == math.MaxInt64 {
		// Detection needs a probe byte at limit+1; at MaxInt64 that offset does
		// not exist, so accepting the call would mean truncating a body one byte
		// over the limit without reporting it. Refuse instead of degrading.
		return nil, errors.New("response body limit must be below MaxInt64")
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}
	return body, nil
}
