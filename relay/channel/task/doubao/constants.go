package doubao

var ModelList = []string{
	"doubao-seedance-1-0-pro-250528",
	"doubao-seedance-1-0-lite-t2v",
	"doubao-seedance-1-0-lite-i2v",
	"doubao-seedance-1-5-pro-251215",
	"doubao-seedance-2-0-260128",
	"doubao-seedance-2-0-fast-260128",
}

var ChannelName = "doubao-video"

// videoInputRatioMap 表示视频输入相对于文生视频的价格折扣。
// 管理员应将基础 ModelRatio 配成无视频输入的价格，检测到 video_url 时再乘上折扣。
var videoInputRatioMap = map[string]float64{
	"doubao-seedance-2-0-260128":      28.0 / 46.0,
	"doubao-seedance-2-0-fast-260128": 22.0 / 37.0,
}

func GetVideoInputRatio(modelName string) (float64, bool) {
	ratio, ok := videoInputRatioMap[modelName]
	return ratio, ok
}
