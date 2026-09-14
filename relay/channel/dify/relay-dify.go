package dify

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

const (
	// Dify upload responses are small JSON envelopes. Keep their error and
	// success bodies bounded independently from the potentially large chat
	// answer body.
	maxDifyUploadResponseBodyBytes int64 = 1 << 20
	maxDifyResponseBodyBytes       int64 = 64 << 20
)

func readDifyResponseBody(reader io.Reader, limit int64) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("Dify response body is empty")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("Dify response body limit must be positive")
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("Dify response body exceeds %d bytes", limit)
	}
	return body, nil
}

func uploadDifyFile(c *gin.Context, info *relaycommon.RelayInfo, user string, media dto.MediaContent) (*DifyFile, error) {
	if info == nil {
		return nil, fmt.Errorf("cannot upload Dify file without relay info")
	}
	uploadUrl := fmt.Sprintf("%s/v1/files/upload", info.ChannelBaseUrl)
	switch media.Type {
	case dto.ContentTypeImageURL:
		// Decode base64 data
		imageMedia := media.GetImageMedia()
		if imageMedia == nil || strings.TrimSpace(imageMedia.Url) == "" {
			return nil, fmt.Errorf("Dify image upload requires a non-empty image URL or data URI")
		}
		base64Data := imageMedia.Url
		// Remove base64 prefix if exists (e.g., "data:image/jpeg;base64,")
		if idx := strings.Index(base64Data, ","); idx != -1 {
			base64Data = base64Data[idx+1:]
		}

		// Decode base64 string
		decodedData, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode image base64: %w", err)
		}

		// Create temporary file
		tempFile, err := os.CreateTemp("", "dify-upload-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp file: %w", err)
		}
		defer tempFile.Close()
		defer os.Remove(tempFile.Name())

		// Write decoded data to temp file
		if _, err := tempFile.Write(decodedData); err != nil {
			return nil, fmt.Errorf("failed to write temporary image: %w", err)
		}

		// Create multipart form
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Add user field
		if err := writer.WriteField("user", user); err != nil {
			return nil, fmt.Errorf("failed to add Dify upload user field: %w", err)
		}

		// Create form file with proper mime type
		mimeType := imageMedia.MimeType
		if mimeType == "" {
			mimeType = "image/jpeg" // default mime type
		}

		// Create form file
		part, err := writer.CreateFormFile("file", fmt.Sprintf("image.%s", strings.TrimPrefix(mimeType, "image/")))
		if err != nil {
			return nil, fmt.Errorf("failed to create Dify upload file part: %w", err)
		}

		// Copy file content to form
		if _, err = io.Copy(part, bytes.NewReader(decodedData)); err != nil {
			return nil, fmt.Errorf("failed to copy image content: %w", err)
		}
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("failed to close Dify upload form: %w", err)
		}

		// Create HTTP request
		req, err := http.NewRequest("POST", uploadUrl, body)
		if err != nil {
			return nil, fmt.Errorf("failed to create Dify upload request: %w", err)
		}

		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", info.ApiKey))
		headerOverride, err := channel.ResolveHeaderOverride(info, c)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve Dify upload headers: %w", err)
		}
		channel.ApplyHeaderOverrideToRequest(req, headerOverride)

		// Send request
		client, err := service.GetHttpClientWithProxy(info.ChannelSetting.Proxy)
		if err != nil {
			return nil, fmt.Errorf("failed to create Dify upload HTTP client: %w", err)
		}
		if client == nil {
			return nil, fmt.Errorf("Dify upload HTTP client is unavailable")
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send Dify upload request: %w", err)
		}
		defer resp.Body.Close()

		responseBody, err := readDifyResponseBody(resp.Body, maxDifyUploadResponseBodyBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to read Dify upload response: %w", err)
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("Dify file upload returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
		}

		var result struct {
			Id string `json:"id"`
		}
		if err := common.Unmarshal(responseBody, &result); err != nil {
			return nil, fmt.Errorf("failed to decode Dify upload response: %w", err)
		}
		if strings.TrimSpace(result.Id) == "" {
			return nil, fmt.Errorf("Dify upload response did not include file id")
		}

		return &DifyFile{
			UploadFileId: result.Id,
			Type:         "image",
			TransferMode: "local_file",
		}, nil
	}
	return nil, fmt.Errorf("Dify file type %q is not supported for upload", media.Type)
}

func requestOpenAI2Dify(c *gin.Context, info *relaycommon.RelayInfo, request dto.GeneralOpenAIRequest) (*DifyChatRequest, error) {
	if err := rejectUnsupportedDifyRequestFields(request); err != nil {
		return nil, err
	}
	if len(request.Messages) == 0 {
		return nil, fmt.Errorf("at least one message is required by Dify Chat")
	}
	difyReq := DifyChatRequest{
		Inputs:           make(map[string]interface{}),
		AutoGenerateName: false,
	}

	user := request.User
	if len(user) == 0 {
		encodedUser, err := common.Marshal(helper.GetResponseID(c))
		if err != nil {
			return nil, fmt.Errorf("encode Dify user id: %w", err)
		}
		user = encodedUser
	}
	var stringUser string
	err := common.Unmarshal(user, &stringUser)
	if err != nil {
		common.SysLog("failed to unmarshal user: " + err.Error())
		stringUser = helper.GetResponseID(c)
	}
	difyReq.User = stringUser

	files := make([]DifyFile, 0)
	var content strings.Builder
	lastConversationRole := ""
	seenUser := false
	for messageIndex, message := range request.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		switch role {
		case "system", "developer", "assistant":
			text, err := channel.OpenAIMessageText(message, "Dify "+role+" history")
			if err != nil {
				return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
			}
			label := "ASSISTANT"
			if role == "system" || role == "developer" {
				label = "SYSTEM"
			}
			content.WriteString(label + ": \n" + text + "\n")
			lastConversationRole = role
		case "user":
			if err := validateDifyMessageEnvelope(message, messageIndex); err != nil {
				return nil, err
			}
			parseContent, err := message.ParseContentStrict()
			if err != nil {
				return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
			}
			if len(parseContent) == 0 {
				return nil, fmt.Errorf("messages[%d]: user content is required by Dify", messageIndex)
			}
			seenUser = true
			for partIndex, mediaContent := range parseContent {
				switch mediaContent.Type {
				case dto.ContentTypeText:
					content.WriteString("USER: \n" + mediaContent.Text + "\n")
				case dto.ContentTypeImageURL:
					media := mediaContent.GetImageMedia()
					if media == nil || strings.TrimSpace(media.Url) == "" {
						return nil, fmt.Errorf("messages[%d].content[%d] image_url.url is required", messageIndex, partIndex)
					}
					var file *DifyFile
					if media.IsRemoteImage() {
						file = &DifyFile{
							Type:         "image",
							TransferMode: "remote_url",
							URL:          media.Url,
						}
					} else {
						file, err = uploadDifyFile(c, info, difyReq.User, mediaContent)
						if err != nil {
							return nil, fmt.Errorf("messages[%d].content[%d]: %w", messageIndex, partIndex, err)
						}
					}
					if file == nil {
						return nil, fmt.Errorf("messages[%d].content[%d] image could not be converted for Dify", messageIndex, partIndex)
					}
					files = append(files, *file)
				default:
					return nil, fmt.Errorf("messages[%d].content[%d] type %q is not supported by Dify", messageIndex, partIndex, mediaContent.Type)
				}
			}
			lastConversationRole = role
		default:
			return nil, fmt.Errorf("messages[%d]: role %q is not supported by Dify Chat", messageIndex, message.Role)
		}
	}
	if !seenUser {
		return nil, fmt.Errorf("at least one user message is required by Dify Chat")
	}
	if lastConversationRole != "user" {
		return nil, fmt.Errorf("final conversation message must have role user for Dify Chat")
	}
	difyReq.Query = content.String()
	difyReq.Files = files
	mode := "blocking"
	if lo.FromPtrOr(request.Stream, false) {
		mode = "streaming"
	}
	difyReq.ResponseMode = mode
	return &difyReq, nil
}

func rejectUnsupportedDifyRequestFields(request dto.GeneralOpenAIRequest) error {
	if request.Prompt != nil || request.Input != nil || request.Instruction != "" {
		return fmt.Errorf("prompt/input fields are not supported by Dify Chat")
	}
	if request.Prefix != nil || request.Suffix != nil {
		return fmt.Errorf("prefix/suffix are not supported by Dify Chat")
	}
	if request.MaxTokens != nil || request.MaxCompletionTokens != nil || request.Temperature != nil ||
		request.TopP != nil || request.TopK != nil || request.Stop != nil || request.FrequencyPenalty != nil ||
		request.PresencePenalty != nil || request.Seed != nil || request.StreamOptions != nil {
		return fmt.Errorf("sampling and stream options are not supported by Dify Chat")
	}
	if request.N != nil && *request.N != 1 {
		return fmt.Errorf("Dify Chat supports only one completion (n=1)")
	}
	if len(request.Tools) > 0 || request.ToolChoice != nil || len(request.Functions) > 0 || len(request.FunctionCall) > 0 {
		return fmt.Errorf("tools and function calling are not supported by Dify Chat")
	}
	if request.ResponseFormat != nil || request.ReasoningEffort != "" || len(request.Verbosity) > 0 || len(request.Reasoning) > 0 ||
		len(request.Modalities) > 0 || len(request.Audio) > 0 || len(request.EncodingFormat) > 0 || request.Dimensions != nil {
		return fmt.Errorf("response format, reasoning, audio, and embedding options are not supported by Dify Chat")
	}
	if len(request.ExtraBody) > 0 || len(request.SearchParameters) > 0 || request.WebSearchOptions != nil || len(request.WebSearch) > 0 ||
		len(request.EnableSearch) > 0 || len(request.SearchDomainFilter) > 0 || len(request.SearchRecencyFilter) > 0 || request.ReturnImages != nil ||
		request.ReturnRelatedQuestions != nil || len(request.SearchMode) > 0 {
		return fmt.Errorf("provider-specific search and extra fields are not supported by Dify Chat")
	}
	if len(request.ServiceTier) > 0 || request.LogProbs != nil || request.TopLogProbs != nil || len(request.SafetyIdentifier) > 0 ||
		len(request.Store) > 0 || request.PromptCacheKey != "" || len(request.PromptCacheRetention) > 0 || len(request.LogitBias) > 0 ||
		len(request.Metadata) > 0 || len(request.Prediction) > 0 || len(request.VlHighResolutionImages) > 0 || len(request.EnableThinking) > 0 ||
		len(request.ChatTemplateKwargs) > 0 || len(request.Think) > 0 || len(request.THINKING) > 0 || len(request.ReasoningSplit) > 0 {
		return fmt.Errorf("request fields are not supported by Dify Chat")
	}
	return nil
}

func validateDifyMessageEnvelope(message dto.Message, messageIndex int) error {
	if message.Name != nil && strings.TrimSpace(*message.Name) != "" {
		return fmt.Errorf("messages[%d]: name is not supported by Dify Chat", messageIndex)
	}
	if message.Prefix != nil || strings.TrimSpace(message.ToolCallId) != "" {
		return fmt.Errorf("messages[%d]: message metadata is not supported by Dify Chat", messageIndex)
	}
	if message.GetReasoningContent() != "" {
		return fmt.Errorf("messages[%d]: reasoning_content is not supported by Dify Chat", messageIndex)
	}
	toolCalls, err := message.ParseToolCallsStrict()
	if err != nil {
		return fmt.Errorf("messages[%d]: %w", messageIndex, err)
	}
	if len(toolCalls) > 0 {
		return fmt.Errorf("messages[%d]: tool_calls are not supported by Dify Chat", messageIndex)
	}
	return nil
}

func streamResponseDify2OpenAI(difyResponse DifyChunkChatCompletionResponse) *dto.ChatCompletionsStreamResponse {
	response := dto.ChatCompletionsStreamResponse{
		Object:  "chat.completion.chunk",
		Created: common.GetTimestamp(),
		Model:   "dify",
	}
	var choice dto.ChatCompletionsStreamResponseChoice
	if strings.HasPrefix(difyResponse.Event, "workflow_") {
		if constant.DifyDebug {
			text := "Workflow: " + difyResponse.Data.WorkflowId
			if difyResponse.Event == "workflow_finished" {
				text += " " + difyResponse.Data.Status
			}
			choice.Delta.SetReasoningContent(text + "\n")
		}
	} else if strings.HasPrefix(difyResponse.Event, "node_") {
		if constant.DifyDebug {
			text := "Node: " + difyResponse.Data.NodeType
			if difyResponse.Event == "node_finished" {
				text += " " + difyResponse.Data.Status
			}
			choice.Delta.SetReasoningContent(text + "\n")
		}
	} else if difyResponse.Event == "message" || difyResponse.Event == "agent_message" {
		if difyResponse.Answer == "<details style=\"color:gray;background-color: #f8f8f8;padding: 8px;border-radius: 4px;\" open> <summary> Thinking... </summary>\n" {
			difyResponse.Answer = "<think>"
		} else if difyResponse.Answer == "</details>" {
			difyResponse.Answer = "</think>"
		}

		choice.Delta.SetContentString(difyResponse.Answer)
	}
	response.Choices = append(response.Choices, choice)
	return &response
}

func difyStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("Dify response is empty"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, err := readDifyResponseBody(resp.Body, maxDifyUploadResponseBodyBytes)
		service.CloseResponseBodyGracefully(resp)
		if err != nil {
			return nil, types.NewOpenAIError(fmt.Errorf("read Dify error response: %w", err), types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
		}
		return nil, types.NewOpenAIError(fmt.Errorf("Dify returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body))), types.ErrorCodeBadResponseBody, resp.StatusCode)
	}
	var responseText string
	usage := &dto.Usage{}
	var nodeToken int
	var streamErr error
	messageEnded := false
	helper.SetEventStreamHeaders(c)
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var difyResponse DifyChunkChatCompletionResponse
		if err := common.Unmarshal([]byte(data), &difyResponse); err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			streamErr = fmt.Errorf("decode Dify stream event: %w", err)
			sr.Stop(streamErr)
			return
		}
		if difyResponse.Event == "message_end" {
			messageEnded = true
			usage = &difyResponse.MetaData.Usage
			sr.Done()
			return
		} else if difyResponse.Event == "error" {
			streamErr = fmt.Errorf("Dify error event")
			sr.Stop(streamErr)
			return
		}
		openaiResponse := *streamResponseDify2OpenAI(difyResponse)
		if len(openaiResponse.Choices) != 0 {
			responseText += openaiResponse.Choices[0].Delta.GetContentString()
			if openaiResponse.Choices[0].Delta.ReasoningContent != nil {
				nodeToken += 1
			}
		}
		if err := helper.ObjectData(c, openaiResponse); err != nil {
			common.SysLog(err.Error())
			streamErr = fmt.Errorf("write Dify stream response: %w", err)
			sr.Stop(streamErr)
		}
	})
	if streamErr == nil {
		if !messageEnded {
			streamErr = fmt.Errorf("Dify stream ended without message_end event")
		} else if err := helper.StringData(c, "[DONE]"); err != nil {
			streamErr = fmt.Errorf("write Dify stream done marker: %w", err)
		}
	}
	if usage.TotalTokens == 0 {
		usage = service.ResponseText2Usage(c, responseText, info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	usage.CompletionTokens += nodeToken
	if streamErr != nil {
		return usage, types.NewOpenAIError(streamErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	return usage, nil
}

func difyHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("Dify response is empty"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	var difyResponse DifyChatCompletionResponse
	responseBody, err := readDifyResponseBody(resp.Body, maxDifyResponseBodyBytes)

	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.CloseResponseBodyGracefully(resp)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, types.NewOpenAIError(fmt.Errorf("Dify returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody))), types.ErrorCodeBadResponseBody, resp.StatusCode)
	}
	err = common.Unmarshal(responseBody, &difyResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	fullTextResponse := dto.OpenAITextResponse{
		Id:      difyResponse.ConversationId,
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Usage:   difyResponse.MetaData.Usage,
	}
	choice := dto.OpenAITextResponseChoice{
		Index: 0,
		Message: dto.Message{
			Role:    "assistant",
			Content: difyResponse.Answer,
		},
		FinishReason: "stop",
	}
	fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	jsonResponse, err := common.Marshal(fullTextResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err := c.Writer.Write(jsonResponse); err != nil {
		return nil, types.NewOpenAIError(fmt.Errorf("write Dify response: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	return &difyResponse.MetaData.Usage, nil
}
