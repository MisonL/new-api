package aws

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	apitypes "github.com/QuantumNous/new-api/types"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockdocument "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/gin-gonic/gin"
)

func doAwsConverseRequest(c *gin.Context, info *relaycommon.RelayInfo, adaptor *Adaptor, requestBody io.Reader) (any, error) {
	if info == nil || info.ChannelMeta == nil {
		return nil, fmt.Errorf("AWS relay info or channel metadata is nil")
	}
	if adaptor == nil {
		return nil, fmt.Errorf("AWS adaptor is nil")
	}
	if requestBody == nil {
		return nil, fmt.Errorf("AWS Converse request body is nil")
	}
	if info != nil && (model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled) {
		return nil, fmt.Errorf("AWS API-key Converse does not support pass-through request bodies; send OpenAI or Claude compatible input")
	}
	body, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, fmt.Errorf("read AWS Converse request body: %w", err)
	}

	modelID := getAwsConverseModelID(info, adaptor.AwsClient.Options().Region)
	var input *bedrockruntime.ConverseInput
	if adaptor.RequestFormat == apitypes.RelayFormatClaude {
		var request dto.ClaudeRequest
		if err := common.Unmarshal(body, &request); err != nil {
			return nil, apitypes.NewError(fmt.Errorf("decode Claude request for AWS Converse: %w", err), apitypes.ErrorCodeBadRequestBody)
		}
		input, err = convertClaudeToConverseInput(c, info, &request, modelID)
	} else {
		var request dto.GeneralOpenAIRequest
		if err := common.Unmarshal(body, &request); err != nil {
			return nil, apitypes.NewError(fmt.Errorf("decode OpenAI request for AWS Converse: %w", err), apitypes.ErrorCodeBadRequestBody)
		}
		input, err = convertOpenAIToConverseInput(c, info, &request, modelID)
	}
	if err != nil {
		return nil, apitypes.NewError(err, apitypes.ErrorCodeConvertRequestFailed)
	}
	if input == nil {
		return nil, apitypes.NewError(fmt.Errorf("AWS Converse conversion returned nil request"), apitypes.ErrorCodeConvertRequestFailed)
	}

	if info != nil {
		if info.IsStream {
			info.UpstreamRequestPath = fmt.Sprintf("/model/%s/converse-stream", modelID)
		} else {
			info.UpstreamRequestPath = fmt.Sprintf("/model/%s/converse", modelID)
		}
	}
	if info != nil && info.IsStream {
		adaptor.AwsReq = &bedrockruntime.ConverseStreamInput{
			ModelId:                           input.ModelId,
			AdditionalModelRequestFields:      input.AdditionalModelRequestFields,
			AdditionalModelResponseFieldPaths: input.AdditionalModelResponseFieldPaths,
			GuardrailConfig:                   nil,
			InferenceConfig:                   input.InferenceConfig,
			Messages:                          input.Messages,
			OutputConfig:                      input.OutputConfig,
			PerformanceConfig:                 input.PerformanceConfig,
			PromptVariables:                   input.PromptVariables,
			RequestMetadata:                   input.RequestMetadata,
			ServiceTier:                       input.ServiceTier,
			System:                            input.System,
			ToolConfig:                        input.ToolConfig,
		}
	} else {
		adaptor.AwsReq = input
	}
	return nil, nil
}

func getAwsConverseModelID(info *relaycommon.RelayInfo, region string) string {
	modelID := ""
	if info != nil {
		modelID = getAwsModelID(info.UpstreamModelName)
	}
	prefix := getAwsRegionPrefix(region)
	if awsModelCanCrossRegion(modelID, prefix) {
		modelID = awsModelCrossRegion(modelID, prefix)
	}
	return modelID
}

func convertClaudeToConverseInput(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest, modelID string) (*bedrockruntime.ConverseInput, error) {
	if request == nil {
		return nil, fmt.Errorf("Claude request is nil")
	}
	if err := rejectUnsupportedClaudeConverseFields(request); err != nil {
		return nil, err
	}
	openAIRequest, err := service.ClaudeToOpenAIRequest(*request, info)
	if err != nil {
		return nil, fmt.Errorf("convert Claude request to OpenAI shape: %w", err)
	}
	var additional map[string]any
	if request.Thinking != nil {
		additional = map[string]any{
			"thinking": map[string]any{
				"type": request.Thinking.Type,
			},
		}
		if request.Thinking.BudgetTokens != nil {
			additional["thinking"].(map[string]any)["budget_tokens"] = *request.Thinking.BudgetTokens
		}
	}
	return buildConverseInput(c, info, openAIRequest, modelID, additional)
}

func rejectUnsupportedClaudeConverseFields(request *dto.ClaudeRequest) error {
	if len(request.Prompt) > 0 || len(request.CacheControl) > 0 || request.InferenceGeo != "" ||
		len(request.ContextManagement) > 0 || len(request.OutputConfig) > 0 || len(request.OutputFormat) > 0 ||
		len(request.Container) > 0 || len(request.McpServers) > 0 || len(request.Metadata) > 0 ||
		len(request.Speed) > 0 || request.ServiceTier != "" {
		return fmt.Errorf("Claude request contains fields that AWS Converse cannot represent")
	}
	return nil
}

func convertOpenAIToConverseInput(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, modelID string) (*bedrockruntime.ConverseInput, error) {
	return buildConverseInput(c, info, request, modelID, nil)
}

func buildConverseInput(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, modelID string, additional map[string]any) (*bedrockruntime.ConverseInput, error) {
	if request == nil {
		return nil, fmt.Errorf("OpenAI request is nil")
	}
	if strings.TrimSpace(modelID) == "" {
		return nil, fmt.Errorf("AWS model id is required")
	}
	if err := rejectUnsupportedOpenAIConverseFields(request, additional != nil); err != nil {
		return nil, err
	}
	if len(request.Messages) == 0 {
		return nil, fmt.Errorf("at least one message is required by AWS Converse")
	}

	messages, system, err := convertOpenAIMessagesToConverse(c, info, request.Messages)
	if err != nil {
		return nil, err
	}
	inferenceConfig, err := buildConverseInferenceConfig(request)
	if err != nil {
		return nil, err
	}
	toolConfig, err := buildConverseToolConfig(request)
	if err != nil {
		return nil, err
	}
	outputConfig, err := buildConverseOutputConfig(request.ResponseFormat)
	if err != nil {
		return nil, err
	}
	if request.TopK != nil {
		if additional == nil {
			additional = map[string]any{}
		}
		additional["top_k"] = *request.TopK
	}

	input := &bedrockruntime.ConverseInput{
		ModelId:         common.GetPointer(modelID),
		Messages:        messages,
		System:          system,
		InferenceConfig: inferenceConfig,
		ToolConfig:      toolConfig,
		OutputConfig:    outputConfig,
	}
	if len(additional) > 0 {
		input.AdditionalModelRequestFields = bedrockdocument.NewLazyDocument(additional)
	}
	return input, nil
}

func rejectUnsupportedOpenAIConverseFields(request *dto.GeneralOpenAIRequest, allowAdditionalReasoning bool) error {
	if request.Prompt != nil || request.Input != nil || request.Instruction != "" {
		return fmt.Errorf("prompt/input fields are not supported by AWS Converse")
	}
	if request.Prefix != nil || request.Suffix != nil {
		return fmt.Errorf("prefix/suffix are not supported by AWS Converse")
	}
	if len(request.Functions) > 0 || len(request.FunctionCall) > 0 {
		return fmt.Errorf("legacy functions/function_call are not supported by AWS Converse")
	}
	if request.N != nil && *request.N != 1 {
		return fmt.Errorf("AWS Converse supports only one completion (n=1)")
	}
	if request.ParallelTooCalls != nil && !*request.ParallelTooCalls {
		return fmt.Errorf("parallel_tool_calls=false cannot be represented by AWS Converse")
	}
	if request.ReasoningEffort != "" || len(request.Verbosity) > 0 || (len(request.Reasoning) > 0 && !allowAdditionalReasoning) {
		return fmt.Errorf("reasoning options are not supported by AWS Converse")
	}
	if len(request.Modalities) > 0 || len(request.Audio) > 0 {
		return fmt.Errorf("audio/modalities are not supported by AWS Converse")
	}
	if len(request.ExtraBody) > 0 || request.WebSearchOptions != nil || len(request.WebSearch) > 0 || len(request.EnableSearch) > 0 {
		return fmt.Errorf("provider-specific search/extra_body fields are not supported by AWS Converse")
	}
	if len(request.SearchParameters) > 0 || len(request.SearchDomainFilter) > 0 || len(request.SearchRecencyFilter) > 0 ||
		request.ReturnImages != nil || request.ReturnRelatedQuestions != nil || len(request.SearchMode) > 0 {
		return fmt.Errorf("search fields are not supported by AWS Converse")
	}
	if len(request.THINKING) > 0 || len(request.Think) > 0 || len(request.ReasoningSplit) > 0 {
		return fmt.Errorf("provider-specific reasoning fields are not supported by AWS Converse")
	}
	if len(request.EncodingFormat) > 0 || request.Dimensions != nil {
		return fmt.Errorf("embedding fields are not supported by AWS Converse")
	}
	if request.ResponseFormat != nil {
		typ := strings.ToLower(strings.TrimSpace(request.ResponseFormat.Type))
		if typ != "" && typ != "text" && typ != "json_schema" {
			return fmt.Errorf("response_format.type %q is not supported by AWS Converse", request.ResponseFormat.Type)
		}
	}
	return nil
}

func convertOpenAIMessagesToConverse(c *gin.Context, info *relaycommon.RelayInfo, messages []dto.Message) ([]bedrocktypes.Message, []bedrocktypes.SystemContentBlock, error) {
	conversation := make([]bedrocktypes.Message, 0, len(messages))
	system := make([]bedrocktypes.SystemContentBlock, 0)
	seenConversation := false
	seenUser := false
	for messageIndex, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		switch role {
		case "system", "developer":
			if seenConversation {
				return nil, nil, fmt.Errorf("messages[%d]: system/developer messages must precede conversation messages for AWS Converse", messageIndex)
			}
			text, err := channelOpenAIMessageText(message, "AWS Converse system")
			if err != nil {
				return nil, nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
			}
			system = append(system, &bedrocktypes.SystemContentBlockMemberText{Value: text})
		case "tool":
			if strings.TrimSpace(message.ToolCallId) == "" {
				return nil, nil, fmt.Errorf("messages[%d]: tool_call_id is required by AWS Converse", messageIndex)
			}
			content, err := convertToolResultContent(message, messageIndex)
			if err != nil {
				return nil, nil, err
			}
			appendConverseMessage(&conversation, bedrocktypes.Message{
				Role: bedrocktypes.ConversationRoleUser,
				Content: []bedrocktypes.ContentBlock{&bedrocktypes.ContentBlockMemberToolResult{
					Value: bedrocktypes.ToolResultBlock{
						ToolUseId: common.GetPointer(message.ToolCallId),
						Status:    bedrocktypes.ToolResultStatusSuccess,
						Content:   content,
					},
				}},
			})
			seenConversation = true
			seenUser = true
		case "user", "assistant":
			if message.Name != nil && strings.TrimSpace(*message.Name) != "" {
				return nil, nil, fmt.Errorf("messages[%d]: name is not supported by AWS Converse", messageIndex)
			}
			if message.Prefix != nil {
				return nil, nil, fmt.Errorf("messages[%d]: prefix is not supported by AWS Converse", messageIndex)
			}
			if strings.TrimSpace(message.ToolCallId) != "" {
				return nil, nil, fmt.Errorf("messages[%d]: tool_call_id is only valid for tool messages", messageIndex)
			}
			if reasoning := message.GetReasoningContent(); reasoning != "" {
				return nil, nil, fmt.Errorf("messages[%d]: reasoning_content cannot be replayed to AWS Converse without its provider signature", messageIndex)
			}
			content, err := message.ParseContentStrict()
			if err != nil {
				return nil, nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
			}
			toolCalls, err := message.ParseToolCallsStrict()
			if err != nil {
				return nil, nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
			}
			if len(toolCalls) > 0 && role != "assistant" {
				return nil, nil, fmt.Errorf("messages[%d]: tool_calls are only valid for assistant messages", messageIndex)
			}
			blocks := make([]bedrocktypes.ContentBlock, 0, len(content)+len(toolCalls))
			for contentIndex, part := range content {
				block, err := convertOpenAIContentBlock(c, info, role, part, messageIndex, contentIndex)
				if err != nil {
					return nil, nil, err
				}
				blocks = append(blocks, block)
			}
			for callIndex, call := range toolCalls {
				block, err := convertOpenAIToolUse(call, messageIndex, callIndex)
				if err != nil {
					return nil, nil, err
				}
				blocks = append(blocks, block)
			}
			if len(blocks) == 0 {
				return nil, nil, fmt.Errorf("messages[%d]: content or tool_calls are required by AWS Converse", messageIndex)
			}
			providerRole := bedrocktypes.ConversationRoleUser
			if role == "assistant" {
				providerRole = bedrocktypes.ConversationRoleAssistant
			} else {
				seenUser = true
			}
			appendConverseMessage(&conversation, bedrocktypes.Message{Role: providerRole, Content: blocks})
			seenConversation = true
		default:
			return nil, nil, fmt.Errorf("messages[%d]: role %q is not supported by AWS Converse", messageIndex, message.Role)
		}
	}
	if !seenUser {
		return nil, nil, fmt.Errorf("at least one user message is required by AWS Converse")
	}
	return conversation, system, nil
}

// appendConverseMessage coalesces adjacent messages with the same role. The
// Converse API requires alternating user/assistant turns, while OpenAI permits
// several adjacent tool results (all represented as user messages).
func appendConverseMessage(messages *[]bedrocktypes.Message, message bedrocktypes.Message) {
	if messages == nil {
		return
	}
	if len(*messages) > 0 && (*messages)[len(*messages)-1].Role == message.Role {
		(*messages)[len(*messages)-1].Content = append((*messages)[len(*messages)-1].Content, message.Content...)
		return
	}
	*messages = append(*messages, message)
}

func channelOpenAIMessageText(message dto.Message, protocol string) (string, error) {
	if message.Name != nil && strings.TrimSpace(*message.Name) != "" {
		return "", fmt.Errorf("name is not supported by %s", protocol)
	}
	if message.Prefix != nil || strings.TrimSpace(message.ToolCallId) != "" {
		return "", fmt.Errorf("message metadata is not supported by %s", protocol)
	}
	if message.GetReasoningContent() != "" {
		return "", fmt.Errorf("reasoning_content is not supported by %s", protocol)
	}
	toolCalls, err := message.ParseToolCallsStrict()
	if err != nil {
		return "", err
	}
	if len(toolCalls) > 0 {
		return "", fmt.Errorf("tool_calls are not supported by %s", protocol)
	}
	parts, err := message.ParseContentStrict()
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for index, part := range parts {
		if part.Type != dto.ContentTypeText {
			return "", fmt.Errorf("content[%d] type %q is not supported by %s", index, part.Type, protocol)
		}
		text.WriteString(part.Text)
	}
	return text.String(), nil
}

func convertToolResultContent(message dto.Message, messageIndex int) ([]bedrocktypes.ToolResultContentBlock, error) {
	parts, err := message.ParseContentStrict()
	if err != nil {
		return nil, fmt.Errorf("messages[%d]: %w", messageIndex, err)
	}
	result := make([]bedrocktypes.ToolResultContentBlock, 0, len(parts))
	for contentIndex, part := range parts {
		if part.Type != dto.ContentTypeText {
			return nil, fmt.Errorf("messages[%d].content[%d] type %q is not supported in AWS Converse tool results", messageIndex, contentIndex, part.Type)
		}
		result = append(result, &bedrocktypes.ToolResultContentBlockMemberText{Value: part.Text})
	}
	if len(result) == 0 {
		result = append(result, &bedrocktypes.ToolResultContentBlockMemberText{Value: ""})
	}
	return result, nil
}

func convertOpenAIContentBlock(c *gin.Context, info *relaycommon.RelayInfo, role string, part dto.MediaContent, messageIndex, contentIndex int) (bedrocktypes.ContentBlock, error) {
	switch part.Type {
	case dto.ContentTypeText:
		return &bedrocktypes.ContentBlockMemberText{Value: part.Text}, nil
	case dto.ContentTypeImageURL:
		if role != "user" {
			return nil, fmt.Errorf("messages[%d].content[%d]: images are only supported in user messages by AWS Converse", messageIndex, contentIndex)
		}
		source := part.ToFileSource()
		if source == nil {
			return nil, fmt.Errorf("messages[%d].content[%d]: image_url has no usable source", messageIndex, contentIndex)
		}
		base64Data, mimeType, err := service.GetBase64Data(c, source, "formatting image for AWS Converse")
		if err != nil {
			return nil, fmt.Errorf("messages[%d].content[%d]: load image failed: %w", messageIndex, contentIndex, err)
		}
		format, err := converseImageFormat(mimeType)
		if err != nil {
			return nil, fmt.Errorf("messages[%d].content[%d]: %w", messageIndex, contentIndex, err)
		}
		base64Data = strings.TrimSpace(base64Data)
		if comma := strings.Index(base64Data, ","); comma >= 0 {
			base64Data = base64Data[comma+1:]
		}
		data, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return nil, fmt.Errorf("messages[%d].content[%d]: image data is not valid base64: %w", messageIndex, contentIndex, err)
		}
		return &bedrocktypes.ContentBlockMemberImage{Value: bedrocktypes.ImageBlock{
			Format: bedrocktypes.ImageFormat(format),
			Source: &bedrocktypes.ImageSourceMemberBytes{Value: data},
		}}, nil
	default:
		return nil, fmt.Errorf("messages[%d].content[%d] type %q is not supported by AWS Converse", messageIndex, contentIndex, part.Type)
	}
}

func converseImageFormat(mimeType string) (string, error) {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if !strings.HasPrefix(mimeType, "image/") {
		return "", fmt.Errorf("image MIME type %q is not supported", mimeType)
	}
	format := strings.TrimPrefix(mimeType, "image/")
	if format == "jpg" {
		format = "jpeg"
	}
	switch format {
	case "png", "jpeg", "gif", "webp":
		return format, nil
	default:
		return "", fmt.Errorf("image format %q is not supported by AWS Converse", format)
	}
}

func convertOpenAIToolUse(call dto.ToolCallRequest, messageIndex, callIndex int) (bedrocktypes.ContentBlock, error) {
	if strings.TrimSpace(call.Type) != "" && call.Type != "function" {
		return nil, fmt.Errorf("messages[%d].tool_calls[%d]: type %q is not supported by AWS Converse", messageIndex, callIndex, call.Type)
	}
	if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Function.Name) == "" {
		return nil, fmt.Errorf("messages[%d].tool_calls[%d]: id and function.name are required by AWS Converse", messageIndex, callIndex)
	}
	input := map[string]any{}
	if strings.TrimSpace(call.Function.Arguments) != "" {
		if err := common.Unmarshal([]byte(call.Function.Arguments), &input); err != nil {
			return nil, fmt.Errorf("messages[%d].tool_calls[%d]: arguments are invalid: %w", messageIndex, callIndex, err)
		}
		if input == nil {
			return nil, fmt.Errorf("messages[%d].tool_calls[%d]: arguments must be a JSON object", messageIndex, callIndex)
		}
	}
	return &bedrocktypes.ContentBlockMemberToolUse{Value: bedrocktypes.ToolUseBlock{
		Input:     bedrockdocument.NewLazyDocument(input),
		Name:      common.GetPointer(call.Function.Name),
		ToolUseId: common.GetPointer(call.ID),
	}}, nil
}

func buildConverseInferenceConfig(request *dto.GeneralOpenAIRequest) (*bedrocktypes.InferenceConfiguration, error) {
	config := &bedrocktypes.InferenceConfiguration{}
	hasConfig := false
	if request.MaxCompletionTokens != nil {
		value := int32(*request.MaxCompletionTokens)
		config.MaxTokens = &value
		hasConfig = true
	} else if request.MaxTokens != nil {
		value := int32(*request.MaxTokens)
		config.MaxTokens = &value
		hasConfig = true
	}
	if request.Temperature != nil {
		value := float32(*request.Temperature)
		config.Temperature = &value
		hasConfig = true
	}
	if request.TopP != nil {
		value := float32(*request.TopP)
		config.TopP = &value
		hasConfig = true
	}
	if request.Stop != nil {
		sequences, err := parseStopSequences(request.Stop)
		if err != nil {
			return nil, err
		}
		config.StopSequences = sequences
		hasConfig = true
	}
	if !hasConfig {
		return nil, nil
	}
	return config, nil
}

func buildConverseToolConfig(request *dto.GeneralOpenAIRequest) (*bedrocktypes.ToolConfiguration, error) {
	if len(request.Tools) == 0 {
		if request.ToolChoice != nil {
			choice, ok := request.ToolChoice.(string)
			if !ok || strings.ToLower(strings.TrimSpace(choice)) != "none" {
				return nil, fmt.Errorf("tool_choice requires tools for AWS Converse")
			}
		}
		return nil, nil
	}
	tools := make([]bedrocktypes.Tool, 0, len(request.Tools))
	for index, tool := range request.Tools {
		if strings.TrimSpace(tool.Type) != "" && tool.Type != "function" {
			return nil, fmt.Errorf("tools[%d].type %q is not supported by AWS Converse", index, tool.Type)
		}
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" {
			return nil, fmt.Errorf("tools[%d].function.name is required by AWS Converse", index)
		}
		schema := tool.Function.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		var schemaMap map[string]any
		if err := commonUnmarshalSchema(schema, &schemaMap); err != nil || schemaMap == nil {
			if err == nil {
				err = fmt.Errorf("schema must be an object")
			}
			return nil, fmt.Errorf("tools[%d].function.parameters: %w", index, err)
		}
		if schemaType, ok := schemaMap["type"].(string); ok && schemaType != "object" {
			return nil, fmt.Errorf("tools[%d].function.parameters.type must be object", index)
		}
		if _, ok := schemaMap["type"]; !ok {
			schemaMap["type"] = "object"
		}
		spec := bedrocktypes.ToolSpecification{
			Name:        common.GetPointer(name),
			Description: common.GetPointer(tool.Function.Description),
			InputSchema: &bedrocktypes.ToolInputSchemaMemberJson{Value: bedrockdocument.NewLazyDocument(schemaMap)},
			Strict:      tool.Function.Strict,
		}
		tools = append(tools, &bedrocktypes.ToolMemberToolSpec{Value: spec})
	}
	config := &bedrocktypes.ToolConfiguration{Tools: tools}
	if request.ToolChoice != nil {
		choice, err := convertConverseToolChoice(request.ToolChoice)
		if err != nil {
			return nil, err
		}
		config.ToolChoice = choice
	}
	return config, nil
}

func commonUnmarshalSchema(value any, target *map[string]any) error {
	data, err := common.Marshal(value)
	if err != nil {
		return err
	}
	return common.Unmarshal(data, target)
}

func convertConverseToolChoice(value any) (bedrocktypes.ToolChoice, error) {
	choice, ok := value.(string)
	if ok {
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "auto":
			return &bedrocktypes.ToolChoiceMemberAuto{Value: bedrocktypes.AutoToolChoice{}}, nil
		case "required":
			return &bedrocktypes.ToolChoiceMemberAny{Value: bedrocktypes.AnyToolChoice{}}, nil
		case "none":
			return nil, fmt.Errorf("tool_choice=none cannot be combined with tools in AWS Converse")
		default:
			return nil, fmt.Errorf("tool_choice %q is not supported by AWS Converse", choice)
		}
	}
	var object map[string]any
	if err := commonUnmarshalSchema(value, &object); err != nil || object == nil {
		if err == nil {
			err = fmt.Errorf("tool_choice must be a string or object")
		}
		return nil, err
	}
	if typ, _ := object["type"].(string); typ != "" && typ != "function" {
		return nil, fmt.Errorf("tool_choice.type %q is not supported by AWS Converse", typ)
	}
	function, ok := object["function"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool_choice.function is required by AWS Converse")
	}
	name, _ := function["name"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("tool_choice.function.name is required by AWS Converse")
	}
	return &bedrocktypes.ToolChoiceMemberTool{Value: bedrocktypes.SpecificToolChoice{Name: common.GetPointer(name)}}, nil
}

func buildConverseOutputConfig(format *dto.ResponseFormat) (*bedrocktypes.OutputConfig, error) {
	if format == nil {
		return nil, nil
	}
	typ := strings.ToLower(strings.TrimSpace(format.Type))
	if typ == "" || typ == "text" {
		return nil, nil
	}
	if typ != "json_schema" {
		return nil, fmt.Errorf("response_format.type %q cannot be represented by AWS Converse", format.Type)
	}
	if len(format.JsonSchema) == 0 {
		return nil, fmt.Errorf("response_format.json_schema is required")
	}
	var wrapper dto.FormatJsonSchema
	if err := common.Unmarshal(format.JsonSchema, &wrapper); err != nil {
		return nil, fmt.Errorf("response_format.json_schema is invalid: %w", err)
	}
	schema := wrapper.Schema
	if schema == nil {
		var direct map[string]any
		if err := common.Unmarshal(format.JsonSchema, &direct); err == nil {
			schema = direct
		}
	}
	if schema == nil {
		return nil, fmt.Errorf("response_format.json_schema.schema is required")
	}
	schemaBytes, err := common.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("response_format.json_schema.schema is invalid: %w", err)
	}
	name := strings.TrimSpace(wrapper.Name)
	if name == "" {
		name = "response"
	}
	definition := bedrocktypes.JsonSchemaDefinition{
		Name:        common.GetPointer(name),
		Schema:      common.GetPointer(string(schemaBytes)),
		Description: common.GetPointer(wrapper.Description),
	}
	return &bedrocktypes.OutputConfig{TextFormat: &bedrocktypes.OutputFormat{
		Type: bedrocktypes.OutputFormatTypeJsonSchema,
		Structure: &bedrocktypes.OutputFormatStructureMemberJsonSchema{
			Value: definition,
		},
	}}, nil
}

func converseHandler(c *gin.Context, info *relaycommon.RelayInfo, adaptor *Adaptor) (*apitypes.NewAPIError, *dto.Usage) {
	if adaptor == nil || adaptor.AwsClient == nil {
		return apitypes.NewError(fmt.Errorf("AWS Converse client is not initialized"), apitypes.ErrorCodeBadRequestBody), nil
	}
	request, ok := adaptor.AwsReq.(*bedrockruntime.ConverseInput)
	if !ok || request == nil {
		return apitypes.NewError(fmt.Errorf("AWS Converse request is not initialized"), apitypes.ErrorCodeBadRequestBody), nil
	}
	ctx, cancel := newAwsInvokeContext()
	defer cancel()
	if !service.StartUpstreamRateLimitRetryFollowup(c) {
		return service.NewUpstreamRateLimitRetryExhaustedError(), nil
	}
	response, err := adaptor.AwsClient.Converse(ctx, request)
	if err != nil {
		statusCode := getAwsErrorStatusCode(err)
		return apitypes.NewOpenAIError(fmt.Errorf("Converse: %w", err), apitypes.ErrorCodeAwsInvokeError, statusCode), nil
	}
	adaptor.AwsResp = response
	claudeResponse, err := converseOutputToClaudeResponse(c, info, response)
	if err != nil {
		return apitypes.NewError(err, apitypes.ErrorCodeBadResponseBody), nil
	}
	usage := converseUsage(response.Usage)
	if info != nil && info.RelayFormat == apitypes.RelayFormatClaude {
		data, marshalErr := common.Marshal(claudeResponse)
		if marshalErr != nil {
			return apitypes.NewError(marshalErr, apitypes.ErrorCodeBadResponseBody), nil
		}
		c.Writer.Header().Set("Content-Type", "application/json")
		c.Writer.WriteHeader(http.StatusOK)
		if _, err := c.Writer.Write(data); err != nil {
			return apitypes.NewError(err, apitypes.ErrorCodeBadResponseBody), usage
		}
		return nil, usage
	}
	openAIResponse, err := claude.ResponseClaude2OpenAIWithError(claudeResponse)
	if err != nil {
		return apitypes.NewError(err, apitypes.ErrorCodeBadResponseBody), nil
	}
	openAIResponse.Usage = *usage
	data, marshalErr := common.Marshal(openAIResponse)
	if marshalErr != nil {
		return apitypes.NewError(marshalErr, apitypes.ErrorCodeBadResponseBody), nil
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	if _, err := c.Writer.Write(data); err != nil {
		return apitypes.NewError(err, apitypes.ErrorCodeBadResponseBody), usage
	}
	return nil, usage
}

func converseOutputToClaudeResponse(c *gin.Context, info *relaycommon.RelayInfo, response *bedrockruntime.ConverseOutput) (*dto.ClaudeResponse, error) {
	if response == nil {
		return nil, fmt.Errorf("AWS Converse response is nil")
	}
	message, ok := response.Output.(*bedrocktypes.ConverseOutputMemberMessage)
	if !ok || message == nil {
		return nil, fmt.Errorf("AWS Converse response does not contain a message")
	}
	model := ""
	if info != nil {
		model = info.UpstreamModelName
	}
	stopReason, err := converseStopReasonToClaude(response.StopReason)
	if err != nil {
		return nil, err
	}
	result := &dto.ClaudeResponse{
		Id:         helper.GetResponseID(c),
		Type:       "message",
		Role:       "assistant",
		Model:      model,
		StopReason: stopReason,
		Usage:      converseClaudeUsage(response.Usage),
		Content:    make([]dto.ClaudeMediaMessage, 0, len(message.Value.Content)),
	}
	for blockIndex, block := range message.Value.Content {
		switch value := block.(type) {
		case *bedrocktypes.ContentBlockMemberText:
			text := value.Value
			result.Content = append(result.Content, dto.ClaudeMediaMessage{Type: "text", Text: &text})
		case *bedrocktypes.ContentBlockMemberReasoningContent:
			reasoning, err := converseReasoningText(value.Value)
			if err != nil {
				return nil, fmt.Errorf("response content[%d]: %w", blockIndex, err)
			}
			result.Content = append(result.Content, dto.ClaudeMediaMessage{Type: "thinking", Thinking: &reasoning})
		case *bedrocktypes.ContentBlockMemberToolUse:
			if value.Value.ToolUseId == nil || strings.TrimSpace(*value.Value.ToolUseId) == "" || value.Value.Name == nil || strings.TrimSpace(*value.Value.Name) == "" {
				return nil, fmt.Errorf("response content[%d]: tool use id and name are required", blockIndex)
			}
			arguments, err := documentToJSONObject(value.Value.Input)
			if err != nil {
				return nil, fmt.Errorf("response content[%d]: %w", blockIndex, err)
			}
			result.Content = append(result.Content, dto.ClaudeMediaMessage{Type: "tool_use", Id: *value.Value.ToolUseId, Name: *value.Value.Name, Input: arguments})
		default:
			return nil, fmt.Errorf("response content[%d] type %T is not supported", blockIndex, block)
		}
	}
	return result, nil
}

func converseReasoningText(value bedrocktypes.ReasoningContentBlock) (string, error) {
	switch reasoning := value.(type) {
	case *bedrocktypes.ReasoningContentBlockMemberReasoningText:
		if reasoning.Value.Text == nil {
			return "", fmt.Errorf("reasoning text is missing")
		}
		return *reasoning.Value.Text, nil
	case *bedrocktypes.ReasoningContentBlockMemberRedactedContent:
		return "", fmt.Errorf("redacted reasoning cannot be represented by Claude/OpenAI responses")
	default:
		return "", fmt.Errorf("reasoning block type %T is not supported", value)
	}
}

type smithyJSONDocument interface {
	UnmarshalSmithyDocument(interface{}) error
	MarshalSmithyDocument() ([]byte, error)
}

func documentToJSONObject(value smithyJSONDocument) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	var object map[string]any
	if err := value.UnmarshalSmithyDocument(&object); err == nil {
		if object == nil {
			return nil, fmt.Errorf("tool input must be a JSON object")
		}
		return object, nil
	}

	// AWS SDK document marshalers can be used for request fixtures and may not
	// round-trip through UnmarshalSmithyDocument when backed by a pointer to a
	// map. Marshal the document bytes as a standards-compatible fallback.
	raw, err := value.MarshalSmithyDocument()
	if err != nil {
		return nil, fmt.Errorf("tool input is invalid: %w", err)
	}
	if err := common.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("tool input is invalid: %w", err)
	}
	if object == nil {
		return nil, fmt.Errorf("tool input must be a JSON object")
	}
	return object, nil
}

func converseUsage(usage *bedrocktypes.TokenUsage) *dto.Usage {
	result := &dto.Usage{UsageSemantic: "openai", UsageSource: "aws_bedrock"}
	if usage == nil {
		return result
	}
	if usage.InputTokens != nil {
		result.PromptTokens = int(*usage.InputTokens)
		result.InputTokens = int(*usage.InputTokens)
	}
	if usage.OutputTokens != nil {
		result.CompletionTokens = int(*usage.OutputTokens)
		result.OutputTokens = int(*usage.OutputTokens)
	}
	if usage.TotalTokens != nil {
		result.TotalTokens = int(*usage.TotalTokens)
	} else {
		result.TotalTokens = result.PromptTokens + result.CompletionTokens
	}
	return result
}

func converseClaudeUsage(usage *bedrocktypes.TokenUsage) *dto.ClaudeUsage {
	result := &dto.ClaudeUsage{}
	if usage == nil {
		return result
	}
	if usage.InputTokens != nil {
		result.InputTokens = int(*usage.InputTokens)
	}
	if usage.OutputTokens != nil {
		result.OutputTokens = int(*usage.OutputTokens)
	}
	return result
}

type converseStreamState struct {
	ID                  string
	Model               string
	Created             int64
	Usage               *dto.Usage
	ClaudeUsage         *dto.ClaudeUsage
	FinishReason        string
	MessageStarted      bool
	MessageStopped      bool
	MetadataReceived    bool
	ClaudeBlocksStarted map[int32]bool
	OpenAIToolIndexes   map[int32]int
}

func converseStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, adaptor *Adaptor) (*apitypes.NewAPIError, *dto.Usage) {
	if adaptor == nil || adaptor.AwsClient == nil {
		return apitypes.NewError(fmt.Errorf("AWS ConverseStream client is not initialized"), apitypes.ErrorCodeBadRequestBody), nil
	}
	request, ok := adaptor.AwsReq.(*bedrockruntime.ConverseStreamInput)
	if !ok || request == nil {
		return apitypes.NewError(fmt.Errorf("AWS ConverseStream request is not initialized"), apitypes.ErrorCodeBadRequestBody), nil
	}
	ctx, cancel := newAwsInvokeContext()
	defer cancel()
	if !service.StartUpstreamRateLimitRetryFollowup(c) {
		return service.NewUpstreamRateLimitRetryExhaustedError(), nil
	}
	response, err := adaptor.AwsClient.ConverseStream(ctx, request)
	if err != nil {
		statusCode := getAwsErrorStatusCode(err)
		return apitypes.NewOpenAIError(fmt.Errorf("ConverseStream: %w", err), apitypes.ErrorCodeAwsInvokeError, statusCode), nil
	}
	adaptor.AwsResp = response
	if response == nil || response.GetStream() == nil {
		return apitypes.NewError(fmt.Errorf("AWS ConverseStream response has no event stream"), apitypes.ErrorCodeBadResponseBody), nil
	}
	stream := response.GetStream()
	defer stream.Close()
	if info == nil {
		return apitypes.NewError(fmt.Errorf("AWS ConverseStream relay info is nil"), apitypes.ErrorCodeBadResponseBody), nil
	}

	state := &converseStreamState{
		ID:                  helper.GetResponseID(c),
		Model:               info.UpstreamModelName,
		Created:             common.GetTimestamp(),
		Usage:               &dto.Usage{UsageSemantic: "openai", UsageSource: "aws_bedrock"},
		ClaudeUsage:         &dto.ClaudeUsage{},
		ClaudeBlocksStarted: make(map[int32]bool),
		OpenAIToolIndexes:   make(map[int32]int),
	}
	helper.SetEventStreamHeaders(c)
	isClaude := info != nil && info.RelayFormat == apitypes.RelayFormatClaude
	for event := range stream.Events() {
		var eventErr error
		if isClaude {
			eventErr = handleConverseClaudeStreamEvent(c, info, state, event)
		} else {
			eventErr = handleConverseOpenAIStreamEvent(c, info, state, event)
		}
		if eventErr != nil {
			return apitypes.NewOpenAIError(eventErr, apitypes.ErrorCodeBadResponseBody, http.StatusBadGateway), state.Usage
		}
	}
	if err := stream.Err(); err != nil {
		return apitypes.NewOpenAIError(fmt.Errorf("AWS ConverseStream event stream: %w", err), apitypes.ErrorCodeBadResponseBody, http.StatusBadGateway), state.Usage
	}
	return finishConverseStream(c, info, state, isClaude)
}

// finishConverseStream 校验终止事件、发送收尾帧，并在成功返回前记录流的正常结束。
// 到达这里的成功路径必然见过 message_start 与 message_stop（Claude 还要求 metadata），
// 因此终止标记为真；没有这一记录时，健康流会以零值 EndReason 结束，被
// classifyStreamStatus 记成 "error"，渠道健康探针也永远无法标记成功。
func finishConverseStream(c *gin.Context, info *relaycommon.RelayInfo, state *converseStreamState, isClaude bool) (*apitypes.NewAPIError, *dto.Usage) {
	if !state.MessageStarted {
		return apitypes.NewOpenAIError(fmt.Errorf("AWS ConverseStream ended without message_start"), apitypes.ErrorCodeBadResponseBody, http.StatusBadGateway), state.Usage
	}
	if !state.MessageStopped {
		return apitypes.NewOpenAIError(fmt.Errorf("AWS ConverseStream ended without message_stop"), apitypes.ErrorCodeBadResponseBody, http.StatusBadGateway), state.Usage
	}
	if isClaude && !state.MetadataReceived {
		return apitypes.NewOpenAIError(fmt.Errorf("AWS ConverseStream ended without metadata"), apitypes.ErrorCodeBadResponseBody, http.StatusBadGateway), state.Usage
	}
	if !isClaude {
		if info.ShouldIncludeUsage {
			if err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(state.ID, state.Created, state.Model, *state.Usage)); err != nil {
				return apitypes.NewError(err, apitypes.ErrorCodeBadResponseBody), state.Usage
			}
		}
	} else if state.MessageStopped {
		stopReason, stopErr := converseStopReasonToClaude(bedrocktypes.StopReason(state.FinishReason))
		if stopErr != nil {
			return apitypes.NewOpenAIError(stopErr, apitypes.ErrorCodeBadResponseBody, http.StatusBadGateway), state.Usage
		}
		if err := helper.ClaudeData(c, dto.ClaudeResponse{Type: "message_delta", Delta: &dto.ClaudeMediaMessage{StopReason: &stopReason}, Usage: state.ClaudeUsage}); err != nil {
			return apitypes.NewError(err, apitypes.ErrorCodeBadResponseBody), state.Usage
		}
		if err := helper.ClaudeData(c, dto.ClaudeResponse{Type: "message_stop"}); err != nil {
			return apitypes.NewError(err, apitypes.ErrorCodeBadResponseBody), state.Usage
		}
	}
	if !isClaude {
		if err := helper.StringData(c, "[DONE]"); err != nil {
			return apitypes.NewError(err, apitypes.ErrorCodeBadResponseBody), state.Usage
		}
	}
	// 走到这里必然见过 message_start 与 message_stop（Claude 还要求 metadata），
	// 是协议的终止标记，按正常结束记录。
	helper.RecordStreamEnd(c, info, true)
	return nil, state.Usage
}

func handleConverseOpenAIStreamEvent(c *gin.Context, info *relaycommon.RelayInfo, state *converseStreamState, event bedrocktypes.ConverseStreamOutput) error {
	response := &dto.ChatCompletionsStreamResponse{Id: state.ID, Object: "chat.completion.chunk", Created: state.Created, Model: state.Model}
	choice := dto.ChatCompletionsStreamResponseChoice{}
	switch value := event.(type) {
	case *bedrocktypes.ConverseStreamOutputMemberMessageStart:
		if state.MessageStarted {
			return fmt.Errorf("AWS Converse stream contains duplicate message_start")
		}
		state.MessageStarted = true
		if value.Value.Role != bedrocktypes.ConversationRoleAssistant {
			return fmt.Errorf("AWS Converse stream started with role %q", value.Value.Role)
		}
		choice.Delta.Role = "assistant"
		choice.Delta.SetContentString("")
	case *bedrocktypes.ConverseStreamOutputMemberContentBlockStart:
		if value.Value.ContentBlockIndex == nil {
			return fmt.Errorf("content block start is missing index")
		}
		if value.Value.Start == nil {
			// Text blocks have no additional start payload in the Converse
			// event union; their first text delta opens the compatibility block.
			return nil
		}
		switch tool := value.Value.Start.(type) {
		case *bedrocktypes.ContentBlockStartMemberToolUse:
			index := int(*value.Value.ContentBlockIndex)
			if state.OpenAIToolIndexes == nil {
				state.OpenAIToolIndexes = make(map[int32]int)
			}
			if _, exists := state.OpenAIToolIndexes[*value.Value.ContentBlockIndex]; exists {
				return fmt.Errorf("duplicate AWS tool content block index %d", index)
			}
			toolIndex := len(state.OpenAIToolIndexes)
			state.OpenAIToolIndexes[*value.Value.ContentBlockIndex] = toolIndex
			if tool.Value.ToolUseId == nil || tool.Value.Name == nil {
				return fmt.Errorf("tool use start is missing id or name")
			}
			call := dto.ToolCallResponse{ID: *tool.Value.ToolUseId, Type: "function", Function: dto.FunctionResponse{Name: *tool.Value.Name, Arguments: ""}}
			call.SetIndex(toolIndex)
			choice.Delta.ToolCalls = []dto.ToolCallResponse{call}
		case *bedrocktypes.ContentBlockStartMemberImage:
			return fmt.Errorf("generated image content cannot be represented by OpenAI Chat Completions")
		default:
			return fmt.Errorf("content block start type %T is not supported", value.Value.Start)
		}
	case *bedrocktypes.ConverseStreamOutputMemberContentBlockDelta:
		if value.Value.Delta == nil || value.Value.ContentBlockIndex == nil {
			return fmt.Errorf("content block delta is missing index or delta")
		}
		index := int(*value.Value.ContentBlockIndex)
		switch delta := value.Value.Delta.(type) {
		case *bedrocktypes.ContentBlockDeltaMemberText:
			choice.Delta.SetContentString(delta.Value)
		case *bedrocktypes.ContentBlockDeltaMemberReasoningContent:
			reasoning, err := converseReasoningDeltaText(delta.Value)
			if err != nil {
				return err
			}
			choice.Delta.SetReasoningContent(reasoning)
		case *bedrocktypes.ContentBlockDeltaMemberToolUse:
			if delta.Value.Input == nil {
				return fmt.Errorf("tool use delta input is missing")
			}
			toolIndex, exists := state.OpenAIToolIndexes[*value.Value.ContentBlockIndex]
			if !exists {
				return fmt.Errorf("tool use delta has unknown content block index %d", index)
			}
			call := dto.ToolCallResponse{Type: "function", Function: dto.FunctionResponse{Arguments: *delta.Value.Input}}
			call.SetIndex(toolIndex)
			choice.Delta.ToolCalls = []dto.ToolCallResponse{call}
		case *bedrocktypes.ContentBlockDeltaMemberImage:
			return fmt.Errorf("generated image content cannot be represented by OpenAI Chat Completions")
		default:
			return fmt.Errorf("content block delta type %T is not supported", value.Value.Delta)
		}
	case *bedrocktypes.ConverseStreamOutputMemberMessageStop:
		if state.MessageStopped {
			return fmt.Errorf("AWS Converse stream contains duplicate message_stop")
		}
		finish, err := converseStopReasonToOpenAI(value.Value.StopReason)
		if err != nil {
			return err
		}
		state.FinishReason = finish
		choice.FinishReason = &finish
		state.MessageStopped = true
	case *bedrocktypes.ConverseStreamOutputMemberMetadata:
		state.MetadataReceived = true
		if value.Value.Usage != nil {
			state.Usage = converseUsage(value.Value.Usage)
			state.ClaudeUsage = converseClaudeUsage(value.Value.Usage)
		}
	case *bedrocktypes.ConverseStreamOutputMemberContentBlockStop:
		// No OpenAI payload is needed for these structural events.
	case *bedrocktypes.UnknownUnionMember:
		return fmt.Errorf("unknown AWS ConverseStream event %q", value.Tag)
	default:
		return fmt.Errorf("AWS ConverseStream event type %T is not supported", event)
	}
	if len(choice.Delta.ToolCalls) > 0 {
		choice.Delta.Content = nil
	}
	if choice.Delta.Content != nil || choice.Delta.ReasoningContent != nil || choice.Delta.Role != "" || len(choice.Delta.ToolCalls) > 0 || choice.FinishReason != nil {
		response.Choices = []dto.ChatCompletionsStreamResponseChoice{choice}
		return helper.ObjectData(c, response)
	}
	return nil
}

func handleConverseClaudeStreamEvent(c *gin.Context, info *relaycommon.RelayInfo, state *converseStreamState, event bedrocktypes.ConverseStreamOutput) error {
	switch value := event.(type) {
	case *bedrocktypes.ConverseStreamOutputMemberMessageStart:
		if state.MessageStarted {
			return fmt.Errorf("AWS Converse stream contains duplicate message_start")
		}
		state.MessageStarted = true
		if value.Value.Role != bedrocktypes.ConversationRoleAssistant {
			return fmt.Errorf("AWS Converse stream started with role %q", value.Value.Role)
		}
		return helper.ClaudeData(c, dto.ClaudeResponse{Type: "message_start", Message: &dto.ClaudeMediaMessage{Id: state.ID, Type: "message", Role: "assistant", Model: state.Model, Usage: state.ClaudeUsage}})
	case *bedrocktypes.ConverseStreamOutputMemberContentBlockStart:
		if value.Value.ContentBlockIndex == nil {
			return fmt.Errorf("content block start is missing index")
		}
		if value.Value.Start == nil {
			return nil
		}
		index := *value.Value.ContentBlockIndex
		state.ClaudeBlocksStarted[index] = true
		block := &dto.ClaudeMediaMessage{}
		switch start := value.Value.Start.(type) {
		case *bedrocktypes.ContentBlockStartMemberToolUse:
			if start.Value.ToolUseId == nil || start.Value.Name == nil {
				return fmt.Errorf("tool use start is missing id or name")
			}
			block.Type = "tool_use"
			block.Id = *start.Value.ToolUseId
			block.Name = *start.Value.Name
			block.Input = map[string]any{}
		case *bedrocktypes.ContentBlockStartMemberImage:
			return fmt.Errorf("generated image content cannot be represented by Claude Messages")
		default:
			return fmt.Errorf("content block start type %T is not supported", value.Value.Start)
		}
		return helper.ClaudeData(c, dto.ClaudeResponse{Type: "content_block_start", Index: intPointer(int(index)), ContentBlock: block})
	case *bedrocktypes.ConverseStreamOutputMemberContentBlockDelta:
		if value.Value.ContentBlockIndex == nil || value.Value.Delta == nil {
			return fmt.Errorf("content block delta is missing index or payload")
		}
		index := *value.Value.ContentBlockIndex
		delta := &dto.ClaudeMediaMessage{}
		blockType := "text"
		switch item := value.Value.Delta.(type) {
		case *bedrocktypes.ContentBlockDeltaMemberText:
			delta.Type = "text_delta"
			delta.Text = &item.Value
		case *bedrocktypes.ContentBlockDeltaMemberReasoningContent:
			reasoning, err := converseReasoningDeltaText(item.Value)
			if err != nil {
				return err
			}
			delta.Type = "thinking_delta"
			delta.Thinking = &reasoning
			blockType = "thinking"
		case *bedrocktypes.ContentBlockDeltaMemberToolUse:
			if item.Value.Input == nil {
				return fmt.Errorf("tool use delta input is missing")
			}
			delta.Type = "input_json_delta"
			delta.PartialJson = item.Value.Input
			blockType = "tool_use"
		case *bedrocktypes.ContentBlockDeltaMemberImage:
			return fmt.Errorf("generated image content cannot be represented by Claude Messages")
		default:
			return fmt.Errorf("content block delta type %T is not supported", value.Value.Delta)
		}
		if !state.ClaudeBlocksStarted[index] {
			if blockType == "tool_use" {
				return fmt.Errorf("tool use delta arrived without a content block start")
			}
			state.ClaudeBlocksStarted[index] = true
			if err := helper.ClaudeData(c, dto.ClaudeResponse{Type: "content_block_start", Index: intPointer(int(index)), ContentBlock: &dto.ClaudeMediaMessage{Type: blockType}}); err != nil {
				return err
			}
		}
		return helper.ClaudeData(c, dto.ClaudeResponse{Type: "content_block_delta", Index: intPointer(int(index)), Delta: delta})
	case *bedrocktypes.ConverseStreamOutputMemberContentBlockStop:
		if value.Value.ContentBlockIndex == nil {
			return fmt.Errorf("content block stop is missing index")
		}
		return helper.ClaudeData(c, dto.ClaudeResponse{Type: "content_block_stop", Index: intPointer(int(*value.Value.ContentBlockIndex))})
	case *bedrocktypes.ConverseStreamOutputMemberMessageStop:
		if state.MessageStopped {
			return fmt.Errorf("AWS Converse stream contains duplicate message_stop")
		}
		state.FinishReason = string(value.Value.StopReason)
		state.MessageStopped = true
		return nil
	case *bedrocktypes.ConverseStreamOutputMemberMetadata:
		state.MetadataReceived = true
		if value.Value.Usage != nil {
			state.Usage = converseUsage(value.Value.Usage)
			state.ClaudeUsage = converseClaudeUsage(value.Value.Usage)
		}
	case *bedrocktypes.UnknownUnionMember:
		return fmt.Errorf("unknown AWS ConverseStream event %q", value.Tag)
	default:
		return fmt.Errorf("AWS ConverseStream event type %T is not supported", event)
	}
	return nil
}

func converseReasoningDeltaText(value bedrocktypes.ReasoningContentBlockDelta) (string, error) {
	switch delta := value.(type) {
	case *bedrocktypes.ReasoningContentBlockDeltaMemberText:
		return delta.Value, nil
	case *bedrocktypes.ReasoningContentBlockDeltaMemberSignature:
		return "", fmt.Errorf("reasoning signature cannot be represented by OpenAI/Claude compatibility output")
	case *bedrocktypes.ReasoningContentBlockDeltaMemberRedactedContent:
		return "", fmt.Errorf("redacted reasoning cannot be represented by OpenAI/Claude compatibility output")
	default:
		return "", fmt.Errorf("reasoning delta type %T is not supported", value)
	}
}

func converseStopReasonToOpenAI(reason bedrocktypes.StopReason) (string, error) {
	switch reason {
	case bedrocktypes.StopReasonEndTurn, bedrocktypes.StopReasonStopSequence:
		return "stop", nil
	case bedrocktypes.StopReasonToolUse:
		return "tool_calls", nil
	case bedrocktypes.StopReasonMaxTokens, bedrocktypes.StopReasonModelContextWindowExceeded:
		return "length", nil
	case bedrocktypes.StopReasonGuardrailIntervened, bedrocktypes.StopReasonContentFiltered:
		return "content_filter", nil
	case bedrocktypes.StopReasonMalformedModelOutput, bedrocktypes.StopReasonMalformedToolUse:
		return "", fmt.Errorf("AWS Converse stop reason %q indicates malformed model output", reason)
	default:
		return "", fmt.Errorf("AWS Converse stop reason %q is not supported", reason)
	}
}

func converseStopReasonToClaude(reason bedrocktypes.StopReason) (string, error) {
	switch reason {
	case bedrocktypes.StopReasonEndTurn, bedrocktypes.StopReasonToolUse, bedrocktypes.StopReasonMaxTokens, bedrocktypes.StopReasonStopSequence:
		return string(reason), nil
	case bedrocktypes.StopReasonGuardrailIntervened, bedrocktypes.StopReasonContentFiltered, bedrocktypes.StopReasonMalformedModelOutput, bedrocktypes.StopReasonMalformedToolUse:
		return "", fmt.Errorf("AWS Converse stop reason %q cannot be represented as a successful Claude response", reason)
	case bedrocktypes.StopReasonModelContextWindowExceeded:
		return "max_tokens", nil
	default:
		return "", fmt.Errorf("AWS Converse stop reason %q is not supported", reason)
	}
}

func intPointer(value int) *int {
	return &value
}
