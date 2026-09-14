package aws

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
)

type AwsClaudeRequest struct {
	// AnthropicVersion should be "bedrock-2023-05-31"
	AnthropicVersion string              `json:"anthropic_version"`
	AnthropicBeta    common.RawMessage   `json:"anthropic_beta,omitempty"`
	System           any                 `json:"system,omitempty"`
	Messages         []dto.ClaudeMessage `json:"messages"`
	MaxTokens        uint                `json:"max_tokens,omitempty"`
	Temperature      *float64            `json:"temperature,omitempty"`
	TopP             float64             `json:"top_p,omitempty"`
	TopK             int                 `json:"top_k,omitempty"`
	StopSequences    []string            `json:"stop_sequences,omitempty"`
	Tools            any                 `json:"tools,omitempty"`
	ToolChoice       any                 `json:"tool_choice,omitempty"`
	Thinking         *dto.Thinking       `json:"thinking,omitempty"`
	OutputConfig     common.RawMessage   `json:"output_config,omitempty"`
	//Metadata         json.RawMessage     `json:"metadata,omitempty"`
}

func formatRequest(requestBody io.Reader, requestHeader http.Header) (*AwsClaudeRequest, error) {
	var awsClaudeRequest AwsClaudeRequest
	err := common.DecodeJson(requestBody, &awsClaudeRequest)
	if err != nil {
		return nil, err
	}
	awsClaudeRequest.AnthropicVersion = "bedrock-2023-05-31"

	// check header anthropic-beta
	anthropicBetaValues := requestHeader.Get("anthropic-beta")
	if len(anthropicBetaValues) > 0 {
		var tempArray []string
		tempArray = strings.Split(anthropicBetaValues, ",")
		if len(tempArray) > 0 {
			betaJson, err := common.Marshal(tempArray)
			if err != nil {
				return nil, err
			}
			awsClaudeRequest.AnthropicBeta = betaJson
		}
	}
	logger.LogJson(context.Background(), "json", awsClaudeRequest)
	return &awsClaudeRequest, nil
}

// NovaMessage Nova模型使用messages-v1格式
type NovaMessage struct {
	Role    string        `json:"role"`
	Content []NovaContent `json:"content"`
}

type NovaContent struct {
	Text string `json:"text"`
}

type NovaRequest struct {
	SchemaVersion   string               `json:"schemaVersion"` // 请求版本，例如 "1.0"
	System          []NovaContent        `json:"system,omitempty"`
	Messages        []NovaMessage        `json:"messages"`                  // 对话消息列表
	InferenceConfig *NovaInferenceConfig `json:"inferenceConfig,omitempty"` // 推理配置，可选
}

type NovaInferenceConfig struct {
	MaxTokens     *int     `json:"maxTokens,omitempty"`     // 最大生成的 token 数
	Temperature   *float64 `json:"temperature,omitempty"`   // 随机性 (默认 0.7, 范围 0-1)
	TopP          *float64 `json:"topP,omitempty"`          // nucleus sampling (默认 0.9, 范围 0-1)
	TopK          *int     `json:"topK,omitempty"`          // 限制候选 token 数 (默认 50, 范围 0-128)
	StopSequences []string `json:"stopSequences,omitempty"` // 停止生成的序列
}

// 转换OpenAI请求为Nova格式
func convertToNovaRequest(req *dto.GeneralOpenAIRequest) (*NovaRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if err := channel.RejectUnsupportedTextRequestFields(*req, "Amazon Nova"); err != nil {
		return nil, err
	}
	if req.StreamOptions != nil || len(req.ExtraBody) > 0 || len(req.WebSearch) > 0 || len(req.EnableSearch) > 0 {
		return nil, fmt.Errorf("request contains options that are not supported by Amazon Nova")
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("at least one message is required by Amazon Nova")
	}

	roles := map[string]string{
		"system":    "system",
		"developer": "system",
		"user":      "user",
		"assistant": "assistant",
	}
	novaMessages := make([]NovaMessage, 0, len(req.Messages))
	systemParts := make([]NovaContent, 0)
	seenConversation := false
	seenUser := false
	for messageIndex, msg := range req.Messages {
		role, text, err := channel.OpenAIMessageRoleText(msg, "Amazon Nova", roles)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
		}
		if role == "system" {
			if seenConversation {
				return nil, fmt.Errorf("messages[%d]: system/developer messages must be first for Amazon Nova", messageIndex)
			}
			systemParts = append(systemParts, NovaContent{Text: text})
			continue
		}
		seenConversation = true
		if role == "user" {
			seenUser = true
		}
		novaMessages = append(novaMessages, NovaMessage{Role: role, Content: []NovaContent{{Text: text}}})
	}
	if !seenUser {
		return nil, fmt.Errorf("at least one user message is required by Amazon Nova")
	}

	novaReq := &NovaRequest{
		SchemaVersion: "messages-v1",
		System:        systemParts,
		Messages:      novaMessages,
	}

	// 设置推理配置。使用指针保留显式零值，避免转换后改变请求语义。
	if req.MaxTokens != nil || req.MaxCompletionTokens != nil || req.Temperature != nil || req.TopP != nil || req.TopK != nil || req.Stop != nil {
		novaReq.InferenceConfig = &NovaInferenceConfig{}
		if req.MaxCompletionTokens != nil {
			value := int(*req.MaxCompletionTokens)
			novaReq.InferenceConfig.MaxTokens = &value
		} else if req.MaxTokens != nil {
			value := int(*req.MaxTokens)
			novaReq.InferenceConfig.MaxTokens = &value
		}
		if req.Temperature != nil {
			value := *req.Temperature
			novaReq.InferenceConfig.Temperature = &value
		}
		if req.TopP != nil {
			value := *req.TopP
			novaReq.InferenceConfig.TopP = &value
		}
		if req.TopK != nil {
			value := *req.TopK
			novaReq.InferenceConfig.TopK = &value
		}
		if req.Stop != nil {
			stopSequences, err := parseStopSequences(req.Stop)
			if err != nil {
				return nil, err
			}
			novaReq.InferenceConfig.StopSequences = stopSequences
		}
	}

	return novaReq, nil
}

// parseStopSequences 解析停止序列，支持字符串或字符串数组。
func parseStopSequences(stop any) ([]string, error) {
	if stop == nil {
		return nil, nil
	}

	switch v := stop.(type) {
	case string:
		if v == "" {
			return nil, nil
		}
		return []string{v}, nil
	case []string:
		if len(v) == 0 {
			return nil, nil
		}
		for index, item := range v {
			if item == "" {
				return nil, fmt.Errorf("stop[%d] must not be empty", index)
			}
		}
		return v, nil
	case []interface{}:
		if len(v) == 0 {
			return nil, nil
		}
		sequences := make([]string, 0, len(v))
		for index, item := range v {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("stop[%d] must be a string", index)
			}
			if str == "" {
				return nil, fmt.Errorf("stop[%d] must not be empty", index)
			}
			sequences = append(sequences, str)
		}
		return sequences, nil
	case common.RawMessage:
		var sequences []string
		if err := common.Unmarshal(v, &sequences); err != nil {
			return nil, fmt.Errorf("stop must be a string or string array: %w", err)
		}
		return parseStopSequences(sequences)
	default:
		return nil, fmt.Errorf("stop must be a string or string array")
	}
}
