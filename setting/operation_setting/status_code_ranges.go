package operation_setting

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/types"
)

type StatusCodeRange struct {
	Start int
	End   int
}

// 这两个规则切片在中继热路径上被 ShouldRetryByStatusCode /
// ShouldDisableByStatusCode 无锁读取，而管理端保存设置会通过
// Automatic*StatusCodesFromString 整体替换。切片头是三字（指针/长度/容量），
// 直接赋值与并发读取构成数据竞争，撕裂读可能把新指针和旧长度拼在一起。
//
// 写方解析出一份新切片后原子发布，读者只 Load 当前指针，旧切片在替换后不再
// 被改写，因此读取端无需加锁。这与 tool_price_setting 的索引发布方式一致。
//
// default* 两份是出厂规则，只在包初始化时构造、之后不再改写，供尚未发布过
// 任何规则时兜底；显式发布空规则会存成空切片而不是留在 nil，因此空切片不会被
// 误当成"未配置"而回退到出厂规则。
var (
	defaultAutomaticRetryStatusCodeRanges = []StatusCodeRange{
		{Start: 100, End: 199},
		{Start: 300, End: 399},
		{Start: 400, End: 407},
		{Start: 409, End: 499},
		{Start: 500, End: 503},
		{Start: 505, End: 523},
		{Start: 525, End: 599},
	}
	defaultAutomaticDisableStatusCodeRanges = []StatusCodeRange{{Start: 401, End: 401}}

	automaticRetryStatusCodeRanges   atomic.Pointer[[]StatusCodeRange]
	automaticDisableStatusCodeRanges atomic.Pointer[[]StatusCodeRange]
)

func storeStatusCodeRanges(target *atomic.Pointer[[]StatusCodeRange], ranges []StatusCodeRange) {
	if ranges == nil {
		ranges = []StatusCodeRange{}
	}
	target.Store(&ranges)
}

func loadStatusCodeRanges(source *atomic.Pointer[[]StatusCodeRange], fallback []StatusCodeRange) []StatusCodeRange {
	if published := source.Load(); published != nil {
		return *published
	}
	return fallback
}

var alwaysSkipRetryStatusCodes = map[int]struct{}{
	504: {},
	524: {},
}

var alwaysSkipRetryCodes = map[types.ErrorCode]struct{}{
	types.ErrorCodeBadResponseBody: {},
}

// AutomaticRetryStatusCodeRanges 返回当前生效的重试状态码规则。返回值是已发布
// 切片本身，读者只能读取，不能改写。
func AutomaticRetryStatusCodeRanges() []StatusCodeRange {
	return loadStatusCodeRanges(&automaticRetryStatusCodeRanges, defaultAutomaticRetryStatusCodeRanges)
}

// SetAutomaticRetryStatusCodeRanges 原子发布一份新的重试状态码规则。
// 生产写入走 AutomaticRetryStatusCodesFromString；此函数供需要直接播种规则的
// 调用方使用（例如渠道自愈相关用例）。
func SetAutomaticRetryStatusCodeRanges(ranges []StatusCodeRange) {
	storeStatusCodeRanges(&automaticRetryStatusCodeRanges, append([]StatusCodeRange(nil), ranges...))
}

// AutomaticDisableStatusCodeRanges 返回当前生效的自动禁用状态码规则。
func AutomaticDisableStatusCodeRanges() []StatusCodeRange {
	return loadStatusCodeRanges(&automaticDisableStatusCodeRanges, defaultAutomaticDisableStatusCodeRanges)
}

// SetAutomaticDisableStatusCodeRanges 原子发布一份新的自动禁用状态码规则。
func SetAutomaticDisableStatusCodeRanges(ranges []StatusCodeRange) {
	storeStatusCodeRanges(&automaticDisableStatusCodeRanges, append([]StatusCodeRange(nil), ranges...))
}

func AutomaticDisableStatusCodesToString() string {
	return statusCodeRangesToString(AutomaticDisableStatusCodeRanges())
}

func AutomaticDisableStatusCodesFromString(s string) error {
	ranges, err := ParseHTTPStatusCodeRanges(s)
	if err != nil {
		return err
	}
	SetAutomaticDisableStatusCodeRanges(ranges)
	return nil
}

func ShouldDisableByStatusCode(code int) bool {
	return shouldMatchStatusCodeRanges(AutomaticDisableStatusCodeRanges(), code)
}

func AutomaticRetryStatusCodesToString() string {
	return statusCodeRangesToString(AutomaticRetryStatusCodeRanges())
}

func AutomaticRetryStatusCodesFromString(s string) error {
	ranges, err := ParseHTTPStatusCodeRanges(s)
	if err != nil {
		return err
	}
	SetAutomaticRetryStatusCodeRanges(ranges)
	return nil
}

func IsAlwaysSkipRetryStatusCode(code int) bool {
	_, exists := alwaysSkipRetryStatusCodes[code]
	return exists
}

func IsAlwaysSkipRetryCode(errorCode types.ErrorCode) bool {
	_, exists := alwaysSkipRetryCodes[errorCode]
	return exists
}

func ShouldRetryByStatusCode(code int) bool {
	if IsAlwaysSkipRetryStatusCode(code) {
		return false
	}
	return shouldMatchStatusCodeRanges(AutomaticRetryStatusCodeRanges(), code)
}

func statusCodeRangesToString(ranges []StatusCodeRange) string {
	if len(ranges) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if r.Start == r.End {
			parts = append(parts, strconv.Itoa(r.Start))
			continue
		}
		parts = append(parts, fmt.Sprintf("%d-%d", r.Start, r.End))
	}
	return strings.Join(parts, ",")
}

func shouldMatchStatusCodeRanges(ranges []StatusCodeRange, code int) bool {
	if code < 100 || code > 599 {
		return false
	}
	for _, r := range ranges {
		if code < r.Start {
			return false
		}
		if code <= r.End {
			return true
		}
	}
	return false
}

func ParseHTTPStatusCodeRanges(input string) ([]StatusCodeRange, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	input = strings.NewReplacer("，", ",").Replace(input)
	segments := strings.Split(input, ",")

	var ranges []StatusCodeRange
	var invalid []string

	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		r, err := parseHTTPStatusCodeToken(seg)
		if err != nil {
			invalid = append(invalid, seg)
			continue
		}
		ranges = append(ranges, r)
	}

	if len(invalid) > 0 {
		return nil, fmt.Errorf("invalid http status code rules: %s", strings.Join(invalid, ", "))
	}
	if len(ranges) == 0 {
		return nil, nil
	}

	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].Start == ranges[j].Start {
			return ranges[i].End < ranges[j].End
		}
		return ranges[i].Start < ranges[j].Start
	})

	merged := []StatusCodeRange{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r.Start <= last.End+1 {
			if r.End > last.End {
				last.End = r.End
			}
			continue
		}
		merged = append(merged, r)
	}

	return merged, nil
}

func parseHTTPStatusCodeToken(token string) (StatusCodeRange, error) {
	token = strings.TrimSpace(token)
	token = strings.ReplaceAll(token, " ", "")
	if token == "" {
		return StatusCodeRange{}, fmt.Errorf("empty token")
	}

	if strings.Contains(token, "-") {
		parts := strings.Split(token, "-")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return StatusCodeRange{}, fmt.Errorf("invalid range token: %s", token)
		}
		start, err := strconv.Atoi(parts[0])
		if err != nil {
			return StatusCodeRange{}, fmt.Errorf("invalid range start: %s", token)
		}
		end, err := strconv.Atoi(parts[1])
		if err != nil {
			return StatusCodeRange{}, fmt.Errorf("invalid range end: %s", token)
		}
		if start > end {
			return StatusCodeRange{}, fmt.Errorf("range start > end: %s", token)
		}
		if start < 100 || end > 599 {
			return StatusCodeRange{}, fmt.Errorf("range out of bounds: %s", token)
		}
		return StatusCodeRange{Start: start, End: end}, nil
	}

	code, err := strconv.Atoi(token)
	if err != nil {
		return StatusCodeRange{}, fmt.Errorf("invalid status code: %s", token)
	}
	if code < 100 || code > 599 {
		return StatusCodeRange{}, fmt.Errorf("status code out of bounds: %s", token)
	}
	return StatusCodeRange{Start: code, End: code}, nil
}
