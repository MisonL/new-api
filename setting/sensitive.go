package setting

import (
	"strings"
	"sync"
)

var CheckSensitiveEnabled = true
var CheckSensitiveOnPromptEnabled = true

//var CheckSensitiveOnCompletionEnabled = true

// StopOnSensitiveEnabled 如果检测到敏感词，是否立刻停止生成，否则替换敏感词
var StopOnSensitiveEnabled = true

// StreamCacheQueueLength 流模式缓存队列长度，0表示无缓存
var StreamCacheQueueLength = 0

// SensitiveWords is intentionally empty by default. Deployments may opt in by
// storing a newline-separated list in the SensitiveWords option.
//
// 读写必须持 sensitiveWordsMutex：管理端保存设置时整体替换切片，而每个中继
// 请求都会遍历它做敏感词检测，没有锁就是数据竞争（切片头撕裂可读到越界长度）。
var SensitiveWords = []string{}

var sensitiveWordsMutex sync.RWMutex

func SensitiveWordsToString() string {
	sensitiveWordsMutex.RLock()
	defer sensitiveWordsMutex.RUnlock()
	return strings.Join(SensitiveWords, "\n")
}

func SensitiveWordsFromString(s string) {
	words := make([]string, 0)
	for _, w := range strings.Split(s, "\n") {
		w = strings.TrimSpace(w)
		if w != "" {
			words = append(words, w)
		}
	}
	sensitiveWordsMutex.Lock()
	SensitiveWords = words
	sensitiveWordsMutex.Unlock()
}

// SensitiveWordsSnapshot 返回当前敏感词列表的副本，供请求路径在锁外遍历。
// 直接读包级切片会与 SensitiveWordsFromString 的替换构成数据竞争。
func SensitiveWordsSnapshot() []string {
	sensitiveWordsMutex.RLock()
	defer sensitiveWordsMutex.RUnlock()
	if len(SensitiveWords) == 0 {
		return nil
	}
	return append([]string(nil), SensitiveWords...)
}

func ShouldCheckPromptSensitive() bool {
	return CheckSensitiveEnabled && CheckSensitiveOnPromptEnabled
}

//func ShouldCheckCompletionSensitive() bool {
//	return CheckSensitiveEnabled && CheckSensitiveOnCompletionEnabled
//}
