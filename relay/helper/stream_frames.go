package helper

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// RecordStreamFrame 记账一帧已下发给客户端的上游数据。
//
// 共享的 StreamScannerHandler 会自己维护 ReceivedResponseCount 与首帧时间；
// 自管扫描器的渠道此前两者都不写，导致 classifyStreamStatus 的 partial 分类
// （中断且 received>0）永不命中、首帧延迟恒为空。凡是自己扫描上游流的 handler，
// 每写出一次下游数据就调用一次，口径与共享扫描器一致：
//   - 计数在写出成功之后递增，写失败的帧不算已下发；
//   - 首帧时间同样只在第一次成功写出时记录。
func RecordStreamFrame(info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	info.ReceivedResponseCount++
	info.SetFirstResponseTime()
}
