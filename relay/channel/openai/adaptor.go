package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/ai360"
	"github.com/QuantumNous/new-api/relay/channel/lingyiwanwu"

	//"github.com/QuantumNous/new-api/relay/channel/minimax"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	"github.com/QuantumNous/new-api/relay/channel/xinference"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/common_handler"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	ChannelType    int
	ResponseFormat string
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	// 使用 service.GeminiToOpenAIRequest 转换请求格式
	openaiRequest, err := service.GeminiToOpenAIRequest(request, info)
	if err != nil {
		return nil, err
	}
	return a.ConvertOpenAIRequest(c, info, openaiRequest)
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	//if !strings.Contains(request.Model, "claude") {
	//	return nil, fmt.Errorf("you are using openai channel type with path /v1/messages, only claude model supported convert, but got %s", request.Model)
	//}
	//if common.DebugEnabled {
	//	bodyBytes := []byte(common.GetJsonString(request))
	//	err := os.WriteFile(fmt.Sprintf("claude_request_%s.txt", c.GetString(common.RequestIdKey)), bodyBytes, 0644)
	//	if err != nil {
	//		println(fmt.Sprintf("failed to save request body to file: %v", err))
	//	}
	//}
	aiRequest, err := service.ClaudeToOpenAIRequest(*request, info)
	if err != nil {
		return nil, err
	}
	//if common.DebugEnabled {
	//	println(fmt.Sprintf("convert claude to openai request result: %s", common.GetJsonString(aiRequest)))
	//	// Save request body to file for debugging
	//	bodyBytes := []byte(common.GetJsonString(aiRequest))
	//	err = os.WriteFile(fmt.Sprintf("claude_to_openai_request_%s.txt", c.GetString(common.RequestIdKey)), bodyBytes, 0644)
	//	if err != nil {
	//		println(fmt.Sprintf("failed to save request body to file: %v", err))
	//	}
	//}
	if info.SupportStreamOptions && info.IsStream {
		aiRequest.StreamOptions = &dto.StreamOptions{
			IncludeUsage: true,
		}
	}
	return a.ConvertOpenAIRequest(c, info, aiRequest)
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType

	// initialize ThinkingContentInfo when thinking_to_content is enabled
	if info.ChannelSetting.ThinkingToContent {
		info.ThinkingContentInfo = relaycommon.ThinkingContentInfo{
			IsFirstThinkingContent:  true,
			SendLastThinkingContent: false,
			HasSentThinkingContent:  false,
		}
	}
}

func isAgnesImageModelName(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "agnes-image-")
}

func usesAgnesProtocol(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	if info.ChannelMeta != nil && info.ChannelMeta.ChannelType == constant.ChannelTypeAgnes {
		return true
	}
	return isAgnesImageModelName(relayInfoUpstreamModelName(info))
}

func relayInfoUpstreamModelName(info *relaycommon.RelayInfo) string {
	if info == nil || info.ChannelMeta == nil {
		return ""
	}
	return info.ChannelMeta.UpstreamModelName
}

func agnesImageInputPayload(request dto.ImageRequest) (json.RawMessage, bool, error) {
	candidates := []json.RawMessage{request.Images, request.Image}
	for _, raw := range candidates {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			continue
		}
		switch common.GetJsonType(trimmed) {
		case "array":
			return trimmed, true, nil
		case "string":
			var single string
			if err := common.Unmarshal(trimmed, &single); err != nil {
				return nil, false, fmt.Errorf("decode Agnes image input failed: %w", err)
			}
			payload, err := common.Marshal([]string{single})
			if err != nil {
				return nil, false, fmt.Errorf("marshal Agnes image input failed: %w", err)
			}
			return payload, true, nil
		default:
			return nil, false, fmt.Errorf("unsupported Agnes image input type: %s", common.GetJsonType(trimmed))
		}
	}
	return nil, false, nil
}

func normalizeAgnesImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (dto.ImageRequest, error) {
	modelName := strings.TrimSpace(request.Model)
	if upstreamModelName := strings.TrimSpace(relayInfoUpstreamModelName(info)); upstreamModelName != "" {
		modelName = upstreamModelName
	}
	if !usesAgnesProtocol(info) && !isAgnesImageModelName(modelName) {
		return request, nil
	}

	if info != nil && info.RelayMode == relayconstant.RelayModeImagesEdits && !isJSONRequest(c) {
		return request, errors.New("Agnes image models require JSON image edit requests with public image URLs or data URI base64 inputs")
	}

	extraBody := make(map[string]json.RawMessage)
	if trimmed := bytes.TrimSpace(request.ExtraBody); len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		if err := common.Unmarshal(trimmed, &extraBody); err != nil {
			return request, fmt.Errorf("decode Agnes image extra_body failed: %w", err)
		}
	}

	inputPayload, hasInputPayload, err := agnesImageInputPayload(request)
	if err != nil {
		return request, err
	}
	if hasInputPayload {
		extraBody["image"] = inputPayload
		request.Image = nil
		request.Images = nil
	}

	switch strings.ToLower(strings.TrimSpace(request.ResponseFormat)) {
	case "url":
		rawURL, err := common.Marshal("url")
		if err != nil {
			return request, fmt.Errorf("marshal Agnes image url response_format failed: %w", err)
		}
		extraBody["response_format"] = rawURL
		request.ResponseFormat = ""
		request.ReturnBase64 = nil
	case "b64_json":
		request.ResponseFormat = ""
		if hasInputPayload || (info != nil && info.RelayMode == relayconstant.RelayModeImagesEdits) {
			rawB64, err := common.Marshal("b64_json")
			if err != nil {
				return request, fmt.Errorf("marshal Agnes image b64_json response_format failed: %w", err)
			}
			extraBody["response_format"] = rawB64
			request.ReturnBase64 = nil
		} else {
			request.ReturnBase64 = lo.ToPtr(true)
		}
	}

	if len(extraBody) > 0 {
		rawExtraBody, err := common.Marshal(extraBody)
		if err != nil {
			return request, fmt.Errorf("marshal Agnes image extra_body failed: %w", err)
		}
		request.ExtraBody = rawExtraBody
	}
	return request, nil
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.RelayMode == relayconstant.RelayModeRealtime {
		if strings.HasPrefix(info.ChannelBaseUrl, "https://") {
			baseUrl := strings.TrimPrefix(info.ChannelBaseUrl, "https://")
			baseUrl = "wss://" + baseUrl
			info.ChannelBaseUrl = baseUrl
		} else if strings.HasPrefix(info.ChannelBaseUrl, "http://") {
			baseUrl := strings.TrimPrefix(info.ChannelBaseUrl, "http://")
			baseUrl = "ws://" + baseUrl
			info.ChannelBaseUrl = baseUrl
		}
	}
	switch info.ChannelType {
	case constant.ChannelTypeAzure:
		apiVersion := info.ApiVersion
		if apiVersion == "" {
			apiVersion = constant.AzureDefaultAPIVersion
		}
		// https://learn.microsoft.com/en-us/azure/cognitive-services/openai/chatgpt-quickstart?pivots=rest-api&tabs=command-line#rest-api
		requestURL := strings.Split(info.RequestURLPath, "?")[0]
		requestURL = fmt.Sprintf("%s?api-version=%s", requestURL, apiVersion)
		task := strings.TrimPrefix(requestURL, "/v1/")

		if info.RelayFormat == types.RelayFormatClaude {
			task = strings.TrimPrefix(task, "messages")
			task = "chat/completions" + task
		}

		// 特殊处理 responses API（包含 compact）
		if info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeResponsesCompact {
			responsesApiVersion := "preview"

			subUrl := "/openai/v1/responses"
			if strings.Contains(info.ChannelBaseUrl, "cognitiveservices.azure.com") {
				subUrl = "/openai/responses"
				responsesApiVersion = apiVersion
			}

			if info.ChannelOtherSettings.AzureResponsesVersion != "" {
				responsesApiVersion = info.ChannelOtherSettings.AzureResponsesVersion
			}

			// compact 模式追加 /compact
			if info.RelayMode == relayconstant.RelayModeResponsesCompact {
				subUrl = subUrl + "/compact"
			}

			requestURL = fmt.Sprintf("%s?api-version=%s", subUrl, responsesApiVersion)
			return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, requestURL, info.ChannelType), nil
		}

		model_ := info.UpstreamModelName
		// 2025年5月10日后创建的渠道不移除.
		if info.ChannelCreateTime < constant.AzureNoRemoveDotTime {
			model_ = strings.Replace(model_, ".", "", -1)
		}
		// https://github.com/songquanpeng/one-api/issues/67
		requestURL = fmt.Sprintf("/openai/deployments/%s/%s", model_, task)
		if info.RelayMode == relayconstant.RelayModeRealtime {
			requestURL = fmt.Sprintf("/openai/realtime?deployment=%s&api-version=%s", model_, apiVersion)
		}
		return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, requestURL, info.ChannelType), nil
	//case constant.ChannelTypeMiniMax:
	//	return minimax.GetRequestURL(info)
	case constant.ChannelTypeCustom:
		url := info.ChannelBaseUrl
		url = strings.Replace(url, "{model}", info.UpstreamModelName, -1)
		return url, nil
	default:
		if (info.RelayMode == relayconstant.RelayModeImagesGenerations || info.RelayMode == relayconstant.RelayModeImagesEdits) &&
			usesAgnesProtocol(info) {
			return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, "/v1/images/generations", info.ChannelType), nil
		}
		if relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info) {
			return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, "/v1/responses", info.ChannelType), nil
		}
		if (info.RelayFormat == types.RelayFormatClaude || info.RelayFormat == types.RelayFormatGemini) &&
			info.RelayMode != relayconstant.RelayModeResponses &&
			info.RelayMode != relayconstant.RelayModeResponsesCompact {
			return fmt.Sprintf("%s/v1/chat/completions", info.ChannelBaseUrl), nil
		}
		return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, info.RequestURLPath, info.ChannelType), nil
	}
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, header)
	if info.ChannelType == constant.ChannelTypeAzure {
		header.Set("api-key", info.ApiKey)
		return nil
	}
	if info.ChannelType == constant.ChannelTypeOpenAI && "" != info.Organization {
		header.Set("OpenAI-Organization", info.Organization)
	}
	// 检查 Header Override 是否已设置 Authorization，如果已设置则跳过默认设置
	// 这样可以避免在 Header Override 应用时被覆盖（虽然 Header Override 会在之后应用，但这里作为额外保护）
	hasAuthOverride := false
	if len(info.HeadersOverride) > 0 {
		for k := range info.HeadersOverride {
			if strings.EqualFold(k, "Authorization") {
				hasAuthOverride = true
				break
			}
		}
	}
	if info.RelayMode == relayconstant.RelayModeRealtime {
		swp := c.Request.Header.Get("Sec-WebSocket-Protocol")
		if swp != "" {
			items := []string{
				"realtime",
				"openai-insecure-api-key." + info.ApiKey,
				"openai-beta.realtime-v1",
			}
			header.Set("Sec-WebSocket-Protocol", strings.Join(items, ","))
			//req.Header.Set("Sec-WebSocket-Key", c.Request.Header.Get("Sec-WebSocket-Key"))
			//req.Header.Set("Sec-Websocket-Extensions", c.Request.Header.Get("Sec-Websocket-Extensions"))
			//req.Header.Set("Sec-Websocket-Version", c.Request.Header.Get("Sec-Websocket-Version"))
		} else {
			header.Set("openai-beta", "realtime=v1")
			if !hasAuthOverride {
				header.Set("Authorization", "Bearer "+info.ApiKey)
			}
		}
	} else {
		if !hasAuthOverride {
			header.Set("Authorization", "Bearer "+info.ApiKey)
		}
	}
	if info.ChannelType == constant.ChannelTypeOpenRouter {
		if header.Get("HTTP-Referer") == "" {
			header.Set("HTTP-Referer", "https://www.newapi.ai")
		}
		if header.Get("X-OpenRouter-Title") == "" {
			header.Set("X-OpenRouter-Title", "New API")
		}
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if info.ChannelType != constant.ChannelTypeOpenAI && info.ChannelType != constant.ChannelTypeAzure {
		request.StreamOptions = nil
	}
	if info.ChannelType == constant.ChannelTypeOpenRouter {
		if len(request.Usage) == 0 {
			request.Usage = json.RawMessage(`{"include":true}`)
		}
		// 适配 OpenRouter 的 thinking 后缀
		if !model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) &&
			strings.HasSuffix(info.UpstreamModelName, "-thinking") {
			info.UpstreamModelName = strings.TrimSuffix(info.UpstreamModelName, "-thinking")
			request.Model = info.UpstreamModelName
			if len(request.Reasoning) == 0 {
				reasoning := map[string]any{
					"enabled": true,
				}
				if request.ReasoningEffort != "" && request.ReasoningEffort != "none" {
					reasoning["effort"] = request.ReasoningEffort
				}
				marshal, err := common.Marshal(reasoning)
				if err != nil {
					return nil, fmt.Errorf("error marshalling reasoning: %w", err)
				}
				request.Reasoning = marshal
			}
			// 清空多余的ReasoningEffort
			request.ReasoningEffort = ""
		} else {
			if len(request.Reasoning) == 0 {
				// 适配 OpenAI 的 ReasoningEffort 格式
				if request.ReasoningEffort != "" {
					reasoning := map[string]any{
						"enabled": true,
					}
					if request.ReasoningEffort != "none" {
						reasoning["effort"] = request.ReasoningEffort
						marshal, err := common.Marshal(reasoning)
						if err != nil {
							return nil, fmt.Errorf("error marshalling reasoning: %w", err)
						}
						request.Reasoning = marshal
					}
				}
			}
			request.ReasoningEffort = ""
		}

		// https://docs.anthropic.com/en/api/openai-sdk#extended-thinking-support
		// 没有做排除3.5Haiku等，要出问题再加吧，最佳兼容性（不是
		if request.THINKING != nil && strings.HasPrefix(info.UpstreamModelName, "anthropic") {
			var thinking dto.Thinking // Claude标准Thinking格式
			if err := json.Unmarshal(request.THINKING, &thinking); err != nil {
				return nil, fmt.Errorf("error Unmarshal thinking: %w", err)
			}

			// 只有当 thinking.Type 是 "enabled" 时才处理
			if thinking.Type == "enabled" {
				// 检查 BudgetTokens 是否为 nil
				if thinking.BudgetTokens == nil {
					return nil, fmt.Errorf("BudgetTokens is nil when thinking is enabled")
				}

				reasoning := openrouter.RequestReasoning{
					Enabled:   true,
					MaxTokens: *thinking.BudgetTokens,
				}

				marshal, err := common.Marshal(reasoning)
				if err != nil {
					return nil, fmt.Errorf("error marshalling reasoning: %w", err)
				}

				request.Reasoning = marshal
			}

			// 清空 THINKING
			request.THINKING = nil
		}

	}
	if strings.HasPrefix(info.UpstreamModelName, "o") || strings.HasPrefix(info.UpstreamModelName, "gpt-5") {
		if lo.FromPtrOr(request.MaxCompletionTokens, uint(0)) == 0 && lo.FromPtrOr(request.MaxTokens, uint(0)) != 0 {
			request.MaxCompletionTokens = request.MaxTokens
			request.MaxTokens = nil
		}

		if strings.HasPrefix(info.UpstreamModelName, "o") {
			request.Temperature = nil
		}

		// gpt-5系列模型适配 归零不再支持的参数
		if strings.HasPrefix(info.UpstreamModelName, "gpt-5") {
			request.Temperature = nil
			request.TopP = nil
			request.LogProbs = nil
		}

		// 转换模型推理力度后缀
		effort, originModel := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(info.UpstreamModelName)
		if effort != "" {
			request.ReasoningEffort = effort
			info.UpstreamModelName = originModel
			request.Model = originModel
		}

		info.ReasoningEffort = request.ReasoningEffort

		// o系列模型developer适配（o1-mini除外）
		if !strings.HasPrefix(info.UpstreamModelName, "o1-mini") && !strings.HasPrefix(info.UpstreamModelName, "o1-preview") {
			//修改第一个Message的内容，将system改为developer
			if len(request.Messages) > 0 && request.Messages[0].Role == "system" {
				request.Messages[0].Role = "developer"
			}
		}
	}

	return request, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	a.ResponseFormat = request.ResponseFormat
	if info.RelayMode == relayconstant.RelayModeAudioSpeech {
		jsonData, err := common.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("error marshalling object: %w", err)
		}
		return bytes.NewReader(jsonData), nil
	} else {
		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)

		writer.WriteField("model", request.Model)

		formData, err2 := common.ParseMultipartFormReusable(c)
		if err2 != nil {
			return nil, fmt.Errorf("error parsing multipart form: %w", err2)
		}

		// 打印类似 curl 命令格式的信息
		logger.LogDebug(c.Request.Context(), fmt.Sprintf("--form 'model=\"%s\"'", request.Model))

		// 遍历表单字段并打印输出
		for key, values := range formData.Value {
			if key == "model" {
				continue
			}
			for _, value := range values {
				writer.WriteField(key, value)
				logger.LogDebug(c.Request.Context(), fmt.Sprintf("--form '%s=\"%s\"'", key, value))
			}
		}

		// 从 formData 中获取文件
		fileHeaders := formData.File["file"]
		if len(fileHeaders) == 0 {
			return nil, errors.New("file is required")
		}

		// 使用 formData 中的第一个文件
		fileHeader := fileHeaders[0]
		logger.LogDebug(c.Request.Context(), fmt.Sprintf("--form 'file=@\"%s\"' (size: %d bytes, content-type: %s)",
			fileHeader.Filename, fileHeader.Size, fileHeader.Header.Get("Content-Type")))

		file, err := fileHeader.Open()
		if err != nil {
			return nil, fmt.Errorf("error opening audio file: %v", err)
		}
		defer file.Close()

		part, err := writer.CreateFormFile("file", fileHeader.Filename)
		if err != nil {
			return nil, errors.New("create form file failed")
		}
		if _, err := io.Copy(part, file); err != nil {
			return nil, errors.New("copy file failed")
		}

		// 关闭 multipart 编写器以设置分界线
		writer.Close()
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		logger.LogDebug(c.Request.Context(), fmt.Sprintf("--header 'Content-Type: %s'", writer.FormDataContentType()))
		return &requestBody, nil
	}
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	normalizedRequest, err := normalizeAgnesImageRequest(c, info, request)
	if err != nil {
		return nil, err
	}
	request = normalizedRequest

	switch info.RelayMode {
	case relayconstant.RelayModeImagesEdits:
		if isJSONRequest(c) {
			return request, nil
		}

		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)

		writer.WriteField("model", request.Model)
		// 使用已解析的 multipart 表单，避免重复解析
		mf := c.Request.MultipartForm
		if mf == nil {
			if _, err := c.MultipartForm(); err != nil {
				return nil, errors.New("failed to parse multipart form")
			}
			mf = c.Request.MultipartForm
		}

		// 写入所有非文件字段
		if mf != nil {
			for key, values := range mf.Value {
				if key == "model" {
					continue
				}
				for _, value := range values {
					writer.WriteField(key, value)
				}
			}
		}

		if mf != nil && mf.File != nil {
			// Check if "image" field exists in any form, including array notation
			var imageFiles []*multipart.FileHeader
			var exists bool

			// First check for standard "image" field
			if imageFiles, exists = mf.File["image"]; !exists || len(imageFiles) == 0 {
				// If not found, check for "image[]" field
				if imageFiles, exists = mf.File["image[]"]; !exists || len(imageFiles) == 0 {
					// If still not found, iterate through all fields to find any that start with "image["
					foundArrayImages := false
					for fieldName, files := range mf.File {
						if strings.HasPrefix(fieldName, "image[") && len(files) > 0 {
							foundArrayImages = true
							imageFiles = append(imageFiles, files...)
						}
					}

					// If no image fields found at all
					if !foundArrayImages && (len(imageFiles) == 0) {
						return nil, errors.New("image is required")
					}
				}
			}

			// Process all image files
			for i, fileHeader := range imageFiles {
				file, err := fileHeader.Open()
				if err != nil {
					return nil, fmt.Errorf("failed to open image file %d: %w", i, err)
				}

				// If multiple images, use image[] as the field name
				fieldName := "image"
				if len(imageFiles) > 1 {
					fieldName = "image[]"
				}

				// Determine MIME type based on file extension
				mimeType := detectImageMimeType(fileHeader.Filename)

				// Create a form file with the appropriate content type
				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, fileHeader.Filename))
				h.Set("Content-Type", mimeType)

				part, err := writer.CreatePart(h)
				if err != nil {
					return nil, fmt.Errorf("create form part failed for image %d: %w", i, err)
				}

				if _, err := io.Copy(part, file); err != nil {
					return nil, fmt.Errorf("copy file failed for image %d: %w", i, err)
				}

				// 复制完立即关闭，避免在循环内使用 defer 占用资源
				_ = file.Close()
			}

			// Handle mask file if present
			if maskFiles, exists := mf.File["mask"]; exists && len(maskFiles) > 0 {
				maskFile, err := maskFiles[0].Open()
				if err != nil {
					return nil, errors.New("failed to open mask file")
				}
				// 复制完立即关闭，避免在循环内使用 defer 占用资源

				// Determine MIME type for mask file
				mimeType := detectImageMimeType(maskFiles[0].Filename)

				// Create a form file with the appropriate content type
				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="mask"; filename="%s"`, maskFiles[0].Filename))
				h.Set("Content-Type", mimeType)

				maskPart, err := writer.CreatePart(h)
				if err != nil {
					return nil, errors.New("create form file failed for mask")
				}

				if _, err := io.Copy(maskPart, maskFile); err != nil {
					return nil, errors.New("copy mask file failed")
				}
				_ = maskFile.Close()
			}
		} else {
			return nil, errors.New("no multipart form data found")
		}

		// 关闭 multipart 编写器以设置分界线
		writer.Close()
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return &requestBody, nil

	default:
		return request, nil
	}
}

// detectImageMimeType determines the MIME type based on the file extension
func detectImageMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		// Try to detect from extension if possible
		if strings.HasPrefix(ext, ".jp") {
			return "image/jpeg"
		}
		// Default to png as a fallback
		return "image/png"
	}
}

func isJSONRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	//  转换模型推理力度后缀
	effort, originModel := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(request.Model)
	if effort != "" {
		if request.Reasoning == nil {
			request.Reasoning = &dto.Reasoning{
				Effort: effort,
			}
		} else {
			request.Reasoning.Effort = effort
		}
		request.Model = originModel
	}
	if info != nil && request.Reasoning != nil && request.Reasoning.Effort != "" {
		info.ReasoningEffort = request.Reasoning.Effort
	}
	stripCodexContext := relaycommon.ShouldStripCodexEncryptedContext(info)
	syntheticContinuation := false
	if relaycommon.IsOpenAICompatibleResponses(info) &&
		!relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info) {
		var err error
		syntheticContinuation, err = service.HasLocalSyntheticCompactReferenceWithContext(relaycommon.GinRequestContext(c), request)
		if err != nil {
			return nil, err
		}
	}
	if syntheticContinuation {
		convertedRequest, _, err := service.ApplySyntheticCompactState(relaycommon.GinRequestContext(c), syntheticCompactScopeFromRelayInfo(info), request)
		if err != nil {
			setResponsesPreviousIDActionForError(c, err)
			return nil, err
		}
		common.SetContextKey(c, constant.ContextKeyResponsesPreviousIDAction, "cleared_by_synthetic_restore")
		if stripCodexContext && !relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info) {
			return stripUnsupportedResponsesInputForRequest(info, convertedRequest, stripCodexContext)
		}
		return convertedRequest, nil
	}
	if relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info) {
		requestContext := relaycommon.GinRequestContext(c)
		if shouldRejectResponsesRESTPreviousResponseID(info, request) {
			requestContext = markResponsesCompactVisibleOnlyForUpstreamProfile(c, requestContext)
		}
		convertedRequest, err := service.BuildSyntheticCompactSummaryRequest(requestContext, syntheticCompactScopeFromRelayInfo(info), request)
		if err != nil {
			setResponsesPreviousIDActionForError(c, err)
			return nil, err
		}
		return convertedRequest, nil
	}
	if shouldRejectResponsesRESTPreviousResponseID(info, request) {
		common.SetContextKey(c, constant.ContextKeyResponsesPreviousIDAction, "rejected_by_upstream_profile")
		return nil, fmt.Errorf("%w: previous_response_id is not supported by upstream profile %s over REST; use Responses WebSocket v2 or a native Responses-capable upstream", service.ErrResponsesRESTPreviousIDUnsupported, info.ChannelOtherSettings.NormalizedResponsesUpstreamProfile())
	}
	if strings.TrimSpace(request.PreviousResponseID) != "" {
		common.SetContextKey(c, constant.ContextKeyResponsesPreviousIDAction, "forwarded_upstream")
	}
	if info != nil && stripCodexContext && !relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info) {
		var err error
		request, err = stripUnsupportedResponsesInputForRequest(info, request, stripCodexContext)
		if err != nil {
			return nil, err
		}
	}
	if relaycommon.IsNativeOpenAICompatibleResponsesCompact(info) {
		return request, nil
	}
	if relaycommon.ShouldHandleSyntheticOpenAICompatibleResponses(info) {
		convertedRequest, _, err := service.ApplySyntheticCompactState(relaycommon.GinRequestContext(c), syntheticCompactScopeFromRelayInfo(info), request)
		if err != nil {
			setResponsesPreviousIDActionForError(c, err)
		}
		return convertedRequest, err
	}
	return request, nil
}

func markResponsesCompactVisibleOnlyForUpstreamProfile(c *gin.Context, requestContext context.Context) context.Context {
	if requestContext == nil {
		requestContext = context.Background()
	}
	requestContext = context.WithValue(requestContext, constant.ContextKeyResponsesCompactVisibleOnly, true)
	if c != nil {
		common.SetContextKey(c, constant.ContextKeyResponsesCompactVisibleOnly, true)
		common.SetContextKey(c, constant.ContextKeyResponsesPreviousIDAction, "cleared_by_upstream_profile")
		if c.Request != nil {
			c.Request = c.Request.WithContext(requestContext)
		}
	}
	return requestContext
}

func setResponsesPreviousIDActionForError(c *gin.Context, err error) {
	if c != nil && errors.Is(err, service.ErrSyntheticCompactStateNotFound) {
		common.SetContextKey(c, constant.ContextKeyResponsesPreviousIDAction, "missing_local_synthetic_state")
	}
}

func shouldRejectResponsesRESTPreviousResponseID(info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	if !relaycommon.IsOpenAICompatibleResponses(info) ||
		!info.ChannelOtherSettings.DisallowsResponsesRESTPreviousResponseID() {
		return false
	}
	return strings.TrimSpace(request.PreviousResponseID) != ""
}

func syntheticCompactScopeFromRelayInfo(info *relaycommon.RelayInfo) service.SyntheticCompactStateScope {
	return service.SyntheticCompactScopeFromSource(info)
}

func stripUnsupportedResponsesInputForRequest(info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest, stripEncryptedReasoning bool) (dto.OpenAIResponsesRequest, error) {
	result, err := stripUnsupportedResponsesInput(request.Input, stripEncryptedReasoning)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, err
	}
	if result.removedCount() == 0 {
		return request, nil
	}
	if result.remainingCount == 0 {
		return dto.OpenAIResponsesRequest{}, errors.New("responses encrypted reasoning context is unsupported by this OpenAI-compatible channel and no other input items remain")
	}
	request.Input = result.input
	channelID := 0
	originModelName := ""
	if info != nil {
		originModelName = info.OriginModelName
		if info.ChannelMeta != nil {
			channelID = info.ChannelMeta.ChannelId
		}
	}
	common.SysLog(fmt.Sprintf(
		"responses encrypted reasoning context removed for OpenAI-compatible channel: channel_id=%d model=%s encrypted_reasoning_items=%d remaining_items=%d",
		channelID,
		originModelName,
		result.encryptedReasoningCount,
		result.remainingCount,
	))
	return request, nil
}

type responsesInputStripResult struct {
	input                   json.RawMessage
	encryptedReasoningCount int
	remainingCount          int
}

func (r responsesInputStripResult) removedCount() int {
	return r.encryptedReasoningCount
}

func stripUnsupportedResponsesInput(input json.RawMessage, stripEncryptedReasoning bool) (responsesInputStripResult, error) {
	result := responsesInputStripResult{
		input: input,
	}
	trimmedInput := bytes.TrimSpace(input)
	if len(trimmedInput) == 0 || trimmedInput[0] != '[' {
		return result, nil
	}

	var items []json.RawMessage
	if err := common.Unmarshal(input, &items); err != nil {
		return result, err
	}

	filtered := make([]json.RawMessage, 0, len(items))
	for _, rawItem := range items {
		var item map[string]json.RawMessage
		if err := common.Unmarshal(rawItem, &item); err != nil {
			filtered = append(filtered, rawItem)
			continue
		}
		itemType := responsesItemType(item)
		if stripEncryptedReasoning && itemType == "reasoning" && responsesItemHasEncryptedContent(item) {
			result.encryptedReasoningCount++
			continue
		}
		filtered = append(filtered, rawItem)
	}
	if result.removedCount() == 0 {
		result.remainingCount = len(items)
		return result, nil
	}

	raw, err := common.Marshal(filtered)
	if err != nil {
		return result, err
	}
	result.input = json.RawMessage(raw)
	result.remainingCount = len(filtered)
	return result, nil
}

func responsesItemType(item map[string]json.RawMessage) string {
	rawType := item["type"]
	if len(rawType) == 0 {
		return ""
	}
	var itemType string
	if err := common.Unmarshal(rawType, &itemType); err != nil {
		return ""
	}
	return itemType
}

func responsesItemHasEncryptedContent(item map[string]json.RawMessage) bool {
	raw := bytes.TrimSpace(item["encrypted_content"])
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var encryptedContent string
	if err := common.Unmarshal(raw, &encryptedContent); err == nil {
		return encryptedContent != ""
	}
	return true
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if info.RelayMode == relayconstant.RelayModeAudioTranscription ||
		info.RelayMode == relayconstant.RelayModeAudioTranslation ||
		(info.RelayMode == relayconstant.RelayModeImagesEdits && !isJSONRequest(c)) {
		return channel.DoFormRequest(a, c, info, requestBody)
	} else if info.RelayMode == relayconstant.RelayModeRealtime {
		return channel.DoWssRequest(a, c, info, requestBody)
	} else {
		return channel.DoApiRequest(a, c, info, requestBody)
	}
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch info.RelayMode {
	case relayconstant.RelayModeRealtime:
		err, usage = OpenaiRealtimeHandler(c, info)
	case relayconstant.RelayModeAudioSpeech:
		usage = OpenaiTTSHandler(c, resp, info)
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err, usage = OpenaiSTTHandler(c, resp, info, a.ResponseFormat)
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		usage, err = OpenaiHandlerWithUsage(c, info, resp)
	case relayconstant.RelayModeRerank:
		usage, err = common_handler.RerankHandler(c, info, resp)
	case relayconstant.RelayModeResponses:
		if info.IsStream {
			usage, err = OaiResponsesStreamHandler(c, info, resp)
		} else {
			usage, err = OaiResponsesHandler(c, info, resp)
		}
	case relayconstant.RelayModeResponsesCompact:
		if relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info) {
			usage, err = OaiSyntheticResponsesCompactionHandler(c, info, resp)
		} else {
			usage, err = OaiResponsesCompactionHandler(c, resp)
		}
	default:
		if info.IsStream {
			usage, err = OaiStreamHandler(c, info, resp)
		} else {
			usage, err = OpenaiHandler(c, info, resp)
		}
	}
	return
}

func (a *Adaptor) GetModelList() []string {
	switch a.ChannelType {
	case constant.ChannelTypeAgnes:
		return AgnesModelList
	case constant.ChannelType360:
		return ai360.ModelList
	case constant.ChannelTypeLingYiWanWu:
		return lingyiwanwu.ModelList
	//case constant.ChannelTypeMiniMax:
	//	return minimax.ModelList
	case constant.ChannelTypeXinference:
		return xinference.ModelList
	case constant.ChannelTypeOpenRouter:
		return openrouter.ModelList
	default:
		return ModelList
	}
}

func (a *Adaptor) GetChannelName() string {
	switch a.ChannelType {
	case constant.ChannelTypeAgnes:
		return AgnesChannelName
	case constant.ChannelType360:
		return ai360.ChannelName
	case constant.ChannelTypeLingYiWanWu:
		return lingyiwanwu.ChannelName
	//case constant.ChannelTypeMiniMax:
	//	return minimax.ChannelName
	case constant.ChannelTypeXinference:
		return xinference.ChannelName
	case constant.ChannelTypeOpenRouter:
		return openrouter.ChannelName
	default:
		return ChannelName
	}
}
