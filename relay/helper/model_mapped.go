package helper

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func ModelMappedHelper(c *gin.Context, info *relaycommon.RelayInfo, request dto.Request) error {
	if info.ChannelMeta == nil {
		info.ChannelMeta = &relaycommon.ChannelMeta{}
	}

	isResponsesCompact := info.RelayMode == relayconstant.RelayModeResponsesCompact
	originModelName := info.OriginModelName
	mappingModelName := originModelName
	compactOriginModelName := originModelName
	if isResponsesCompact && strings.HasSuffix(originModelName, ratio_setting.CompactModelSuffix) {
		mappingModelName = strings.TrimSuffix(originModelName, ratio_setting.CompactModelSuffix)
	} else if isResponsesCompact {
		compactOriginModelName = ratio_setting.WithCompactModelSuffix(mappingModelName)
	}

	// map model name
	modelMapping := c.GetString("model_mapping")
	if modelMapping != "" && modelMapping != "{}" {
		modelMap := make(map[string]string)
		err := common.Unmarshal([]byte(modelMapping), &modelMap)
		if err != nil {
			return fmt.Errorf("unmarshal_model_mapping_failed")
		}

		// 支持链式模型重定向，最终使用链尾的模型。
		// compact 模式先允许完整内部模型名命中映射，再回退到剥离 suffix 后的上游默认模型。
		mappingCandidates := []string{mappingModelName}
		if isResponsesCompact && originModelName != mappingModelName {
			mappingCandidates = []string{originModelName, mappingModelName}
		}

		for _, candidate := range mappingCandidates {
			currentModel := candidate
			visitedModels := map[string]bool{
				currentModel: true,
			}
			candidateMapped := false
			for {
				if mappedModel, exists := modelMap[currentModel]; exists && mappedModel != "" {
					// 模型重定向循环检测，避免无限循环
					if visitedModels[mappedModel] {
						if mappedModel == currentModel {
							break
						}
						return errors.New("model_mapping_contains_cycle")
					}
					visitedModels[mappedModel] = true
					currentModel = mappedModel
					candidateMapped = true
				} else {
					break
				}
			}
			if candidateMapped {
				info.IsModelMapped = true
				info.UpstreamModelName = currentModel
				break
			}
		}
	}

	if isResponsesCompact {
		finalUpstreamModelName := mappingModelName
		if info.IsModelMapped && info.UpstreamModelName != "" {
			finalUpstreamModelName = info.UpstreamModelName
		}
		info.UpstreamModelName = finalUpstreamModelName
		info.OriginModelName = compactOriginModelName
	}
	if request != nil {
		request.SetModelName(info.UpstreamModelName)
	}
	return nil
}
