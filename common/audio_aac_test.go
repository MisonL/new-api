package common

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// adtsFrame builds one ADTS frame: a 7-byte header (protection_absent=1) plus
// payloadLen bytes of payload. frameLength overrides the declared length when
// non-zero, so malformed frames can be constructed.
func adtsFrame(sampleIdx, channelConfig, payloadLen int, declaredLength int) []byte {
	frameLength := 7 + payloadLen
	if declaredLength > 0 {
		frameLength = declaredLength
	}
	header := []byte{
		0xFF,
		0xF1, // MPEG-4, layer 0, protection_absent=1
		byte(0x01<<6 | sampleIdx<<2 | (channelConfig>>2)&0x01),
		byte((channelConfig&0x03)<<6 | (frameLength>>11)&0x03),
		byte(frameLength >> 3),
		byte((frameLength&0x07)<<5 | 0x1F),
		0xFC,
	}
	return append(header, bytes.Repeat([]byte{0x00}, payloadLen)...)
}

type aacParseOutcome struct {
	duration float64
	err      error
	panicked bool
}

// parseAACInGoroutine runs getAACDuration off the test goroutine so a hang or a
// panic is reported as a failed assertion instead of wedging or killing the
// whole package run.
func parseAACInGoroutine(t *testing.T, data []byte) (aacParseOutcome, bool) {
	t.Helper()
	results := make(chan aacParseOutcome, 1)
	go func() {
		var outcome aacParseOutcome
		defer func() {
			if recovered := recover(); recovered != nil {
				outcome.panicked = true
			}
			results <- outcome
		}()
		outcome.duration, outcome.err = getAACDuration(bytes.NewReader(data))
	}()

	select {
	case outcome := <-results:
		return outcome, false
	case <-time.After(3 * time.Second):
		return aacParseOutcome{}, true
	}
}

// TestGetAACDurationRejectsZeroLengthADTSFrame 覆盖 Frame_length=0 的 ADTS 头。
//
// gomedia 的 SplitAACFrame 用 start+Frame_length 推进游标；长度为 0 时游标原地
// 踏步，解析永不返回。登录用户上传这样一个 7 字节样本即可占满一个 goroutine。
func TestGetAACDurationRejectsZeroLengthADTSFrame(t *testing.T) {
	data := []byte{0xFF, 0xF1, 0x50, 0x00, 0x00, 0x00, 0x00}

	outcome, timedOut := parseAACInGoroutine(t, data)
	if timedOut {
		t.Fatal("getAACDuration did not return within 3s for a zero-length ADTS frame (infinite loop)")
	}
	require.False(t, outcome.panicked, "zero-length frame must not panic")
	require.Error(t, outcome.err)
	require.Zero(t, outcome.duration)
}

// TestGetAACDurationRejectsFrameOverrunningBuffer 覆盖声明长度超出剩余字节的帧。
//
// gomedia 直接切 frames[start : start+Frame_length]，越界即 panic；gin 只把
// panic 转成 500，属于可被上传触发的拒绝服务面。
func TestGetAACDurationRejectsFrameOverrunningBuffer(t *testing.T) {
	data := adtsFrame(4, 2, 4, 4096)

	outcome, timedOut := parseAACInGoroutine(t, data)
	if timedOut {
		t.Fatal("getAACDuration did not return within 3s for an overrunning ADTS frame")
	}
	require.False(t, outcome.panicked, "overrunning frame length must not panic")
	require.Error(t, outcome.err)
	require.Zero(t, outcome.duration)
}

// TestGetAACDurationRejectsTruncatedHeader 覆盖同步字之后不足 7 字节的情况。
func TestGetAACDurationRejectsTruncatedHeader(t *testing.T) {
	data := []byte{0xFF, 0xF1, 0x50}

	outcome, timedOut := parseAACInGoroutine(t, data)
	if timedOut {
		t.Fatal("getAACDuration did not return within 3s for a truncated ADTS header")
	}
	require.False(t, outcome.panicked, "truncated header must not panic")
	require.Error(t, outcome.err)
	require.Zero(t, outcome.duration)
}

// TestGetAACDurationRejectsUnsupportedSamplingIndex 覆盖采样率索引 13-15。
//
// 这些索引在 gomedia 的 13 项表里不存在，取其值会索引越界 panic。
func TestGetAACDurationRejectsUnsupportedSamplingIndex(t *testing.T) {
	data := adtsFrame(13, 2, 4, 0)

	outcome, timedOut := parseAACInGoroutine(t, data)
	if timedOut {
		t.Fatal("getAACDuration did not return within 3s for an out-of-table sampling index")
	}
	require.False(t, outcome.panicked, "out-of-table sampling index must not panic")
	require.Error(t, outcome.err)
	require.Zero(t, outcome.duration)
}

// TestGetAACDurationCountsValidFrames 守住正常路径：两个 44100Hz 帧各含 1024 采样。
func TestGetAACDurationCountsValidFrames(t *testing.T) {
	data := append(adtsFrame(4, 2, 32, 0), adtsFrame(4, 2, 32, 0)...)

	duration, err := getAACDuration(bytes.NewReader(data))
	require.NoError(t, err)
	require.InDelta(t, float64(2*1024)/44100.0, duration, 1e-9)
}

// TestGetAACDurationSkipsLeadingNonFrameBytes 守住前置非帧数据（如 ID3 标签）仍能被跳过。
func TestGetAACDurationSkipsLeadingNonFrameBytes(t *testing.T) {
	leading := []byte("ID3\x04\x00\x00\x00\x00\x00\x00")
	data := append(append(leading, adtsFrame(4, 2, 32, 0)...), adtsFrame(4, 2, 32, 0)...)

	duration, err := getAACDuration(bytes.NewReader(data))
	require.NoError(t, err)
	require.InDelta(t, float64(2*1024)/44100.0, duration, 1e-9)
}

// TestGetAACDurationRejectsDataWithoutSyncword 守住无同步字输入。
func TestGetAACDurationRejectsDataWithoutSyncword(t *testing.T) {
	duration, err := getAACDuration(bytes.NewReader(bytes.Repeat([]byte{0x11}, 64)))
	require.Error(t, err)
	require.Zero(t, duration)
}
