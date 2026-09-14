package zhipu_4v

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func requestOpenAI2Zhipu(request dto.GeneralOpenAIRequest) (*dto.GeneralOpenAIRequest, error) {
	messages := make([]dto.Message, 0, len(request.Messages))
	for messageIndex, message := range request.Messages {
		if _, err := message.ParseToolCallsStrict(); err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
		}
		if !message.IsStringContent() {
			mediaMessages, err := message.ParseContentStrict()
			if err != nil {
				return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
			}
			for j, mediaMessage := range mediaMessages {
				switch mediaMessage.Type {
				case dto.ContentTypeText:
				case dto.ContentTypeImageURL:
					imageUrl := mediaMessage.GetImageMedia()
					if imageUrl == nil || strings.TrimSpace(imageUrl.Url) == "" {
						return nil, fmt.Errorf("messages[%d].content[%d] image_url.url is required", messageIndex, j)
					}
					// check if base64
					if strings.HasPrefix(imageUrl.Url, "data:image/") {
						// 去除base64数据的URL前缀（如果有）
						if idx := strings.Index(imageUrl.Url, ","); idx != -1 {
							imageUrl.Url = imageUrl.Url[idx+1:]
						}
					}
					mediaMessage.ImageUrl = imageUrl
					mediaMessages[j] = mediaMessage
				default:
					return nil, fmt.Errorf("messages[%d].content[%d] type %q is not supported by Zhipu GLM-4V", messageIndex, j, mediaMessage.Type)
				}
			}
			message.SetMediaContent(mediaMessages)
		}
		messages = append(messages, message)
	}
	var stop []string
	switch value := request.Stop.(type) {
	case nil:
	case string:
		if value == "" {
			return nil, fmt.Errorf("stop must not be empty")
		}
		stop = []string{value}
	case []string:
		for index, item := range value {
			if item == "" {
				return nil, fmt.Errorf("stop[%d] must not be empty", index)
			}
		}
		stop = value
	case []any:
		stop = make([]string, 0, len(value))
		for index, item := range value {
			itemString, ok := item.(string)
			if !ok || itemString == "" {
				return nil, fmt.Errorf("stop[%d] must be a non-empty string", index)
			}
			stop = append(stop, itemString)
		}
	case common.RawMessage:
		var values []string
		if err := common.Unmarshal(value, &values); err != nil {
			return nil, fmt.Errorf("stop must be a string or string array: %w", err)
		}
		stop = values
	default:
		return nil, fmt.Errorf("stop must be a string or string array")
	}
	out := request
	out.Messages = messages
	out.Stop = stop
	if request.MaxTokens != nil || request.MaxCompletionTokens != nil {
		maxTokens := request.GetMaxTokens()
		out.MaxTokens = &maxTokens
		out.MaxCompletionTokens = nil
	}
	return &out, nil
}
