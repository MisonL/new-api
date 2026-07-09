package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const responsesFallbackTextMaxBytes = 1 << 20

const (
	responsesCompletedImageGenerationCallsKey = "responses_completed_image_generation_calls"
	responsesCompletedWebSearchCallCountKey   = "responses_completed_web_search_call_count"
	responsesCompletedFileSearchCallCountKey  = "responses_completed_file_search_call_count"
	responsesObservedImageGenerationCallsKey  = "responses_observed_image_generation_calls"
	responsesObservedWebSearchCallCountKey    = "responses_observed_web_search_call_count"
	responsesObservedFileSearchCallCountKey   = "responses_observed_file_search_call_count"
	responsesObservedWebSearchCallIDsKey      = "responses_observed_web_search_call_ids"
	responsesObservedFileSearchCallIDsKey     = "responses_observed_file_search_call_ids"
	responsesCompletedOutputFinalizedKey      = "responses_completed_output_finalized"
)

type responsesOutputToolCallCounts struct {
	imageGeneration int
	webSearch       int
	fileSearch      int
	imageCalls      []responsesImageGenerationCallSpec
}

type responsesImageGenerationCallSpec struct {
	ID      string
	Quality string
	Size    string
}

type responsesImageGenerationCallTracker struct {
	ByID         map[string][]responsesImageGenerationCallSpec
	BySpec       map[string]int
	ConsumedByID map[string][]responsesImageGenerationCallSpec
}

func appendResponsesFallbackText(builder *strings.Builder, delta string) bool {
	if builder == nil || delta == "" {
		return false
	}
	remaining := responsesFallbackTextMaxBytes - builder.Len()
	if remaining <= 0 {
		return true
	}
	if len(delta) <= remaining {
		builder.WriteString(delta)
		return false
	}
	for _, r := range delta {
		size := utf8.RuneLen(r)
		if size < 0 {
			size = len(string(r))
		}
		if size > remaining {
			return true
		}
		builder.WriteRune(r)
		remaining -= size
	}
	return false
}

func ensureResponsesBuiltInTools(info *relaycommon.RelayInfo) map[string]*relaycommon.BuildInToolInfo {
	if info == nil {
		return nil
	}
	if info.ResponsesUsageInfo == nil {
		info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{}
	}
	if info.ResponsesUsageInfo.BuiltInTools == nil {
		info.ResponsesUsageInfo.BuiltInTools = make(map[string]*relaycommon.BuildInToolInfo)
	}
	return info.ResponsesUsageInfo.BuiltInTools
}

func isExpectedResponsesToolDefinition(toolType string) bool {
	switch toolType {
	case "",
		"function",
		"custom",
		"tool_search",
		"namespace",
		dto.BuildInToolFileSearch:
		return true
	default:
		return false
	}
}

func logUnhandledResponsesToolType(c *gin.Context, toolType string) {
	if isExpectedResponsesToolDefinition(toolType) {
		return
	}
	logger.LogDebug(c, fmt.Sprintf("untracked responses tool type: %s", toolType))
}

func countResponsesBuiltInToolCall(info *relaycommon.RelayInfo, toolType string, tool map[string]any) bool {
	_, ok := countResponsesBuiltInToolCallWithSpec(info, toolType, tool)
	return ok
}

func countResponsesBuiltInToolCallWithSpec(info *relaycommon.RelayInfo, toolType string, tool map[string]any) (responsesImageGenerationCallSpec, bool) {
	builtInTools := ensureResponsesBuiltInTools(info)
	if builtInTools == nil {
		return responsesImageGenerationCallSpec{}, false
	}
	canonicalToolType, ok := relaycommon.CanonicalResponsesBuiltInToolType(toolType)
	if !ok {
		return responsesImageGenerationCallSpec{}, false
	}
	incomingToolInfo := newBuiltInToolInfo(canonicalToolType, tool)
	buildToolinfo, ok := builtInTools[canonicalToolType]
	if !ok || buildToolinfo == nil {
		if incomingToolInfo == nil {
			return responsesImageGenerationCallSpec{}, false
		}
		incomingToolInfo.CallCount = 1
		effectiveSpec := imageGenerationCallSpecFromTool(tool, incomingToolInfo.Quality, incomingToolInfo.Size)
		if canonicalToolType == dto.BuildInToolImageGeneration {
			recordImageGenerationCall(incomingToolInfo, incomingToolInfo.Quality, incomingToolInfo.Size)
		}
		builtInTools[canonicalToolType] = incomingToolInfo
		return effectiveSpec, true
	}
	previousCallCount := buildToolinfo.CallCount
	mergeResponsesBuiltInToolFields(buildToolinfo, incomingToolInfo)
	buildToolinfo.CallCount++
	effectiveSpec := imageGenerationCallSpecFromTool(tool, "", "")
	if canonicalToolType == dto.BuildInToolImageGeneration {
		quality, size := effectiveImageGenerationCallSpec(buildToolinfo, incomingToolInfo)
		recordImageGenerationCall(buildToolinfo, quality, size)
		effectiveSpec.Quality = quality
		effectiveSpec.Size = size
		if previousCallCount == 0 && (incomingToolInfo.Quality != "" || incomingToolInfo.Size != "") {
			buildToolinfo.Quality = quality
			buildToolinfo.Size = size
		}
	}
	return effectiveSpec, true
}

func mergeResponsesBuiltInToolInfo(info *relaycommon.RelayInfo, toolType string, tool map[string]any) bool {
	builtInTools := ensureResponsesBuiltInTools(info)
	if builtInTools == nil {
		return false
	}
	canonicalToolType, ok := relaycommon.CanonicalResponsesBuiltInToolType(toolType)
	if !ok {
		return false
	}
	incomingToolInfo := newBuiltInToolInfo(canonicalToolType, tool)
	if incomingToolInfo == nil {
		return false
	}
	buildToolinfo, ok := builtInTools[canonicalToolType]
	if !ok || buildToolinfo == nil {
		builtInTools[canonicalToolType] = incomingToolInfo
		return true
	}
	mergeResponsesBuiltInToolFields(buildToolinfo, incomingToolInfo)
	return true
}

func mergeResponsesBuiltInToolFields(target *relaycommon.BuildInToolInfo, incoming *relaycommon.BuildInToolInfo) {
	if target == nil || incoming == nil {
		return
	}
	if target.ToolName == "" {
		target.ToolName = incoming.ToolName
	}
	// Search size priority: explicit request value, response tool value, then medium default.
	if target.SearchContextSize == "" {
		target.SearchContextSize = incoming.SearchContextSize
		target.DefaultSearchSize = incoming.DefaultSearchSize
	} else if target.DefaultSearchSize && !incoming.DefaultSearchSize && incoming.SearchContextSize != "" {
		target.SearchContextSize = incoming.SearchContextSize
		target.DefaultSearchSize = false
	}
	if target.Quality == "" {
		target.Quality = incoming.Quality
	}
	if target.Size == "" {
		target.Size = incoming.Size
	}
}

func recordImageGenerationCall(tool *relaycommon.BuildInToolInfo, quality string, size string) {
	if tool == nil {
		return
	}
	if tool.ImageCalls == nil {
		tool.ImageCalls = make(map[string]int)
	}
	tool.ImageCalls[relaycommon.ImageGenerationCallKey(quality, size)]++
}

func imageGenerationCallSpecFromOutput(output *dto.ResponsesOutput) responsesImageGenerationCallSpec {
	if output == nil {
		return responsesImageGenerationCallSpec{}
	}
	return responsesImageGenerationCallSpec{
		ID:      strings.TrimSpace(output.ID),
		Quality: output.Quality,
		Size:    output.Size,
	}
}

func imageGenerationCallSpecFromTool(tool map[string]any, quality string, size string) responsesImageGenerationCallSpec {
	return responsesImageGenerationCallSpec{
		ID:      builtInToolValue(tool, "id"),
		Quality: quality,
		Size:    size,
	}
}

func imageGenerationSpecKey(spec responsesImageGenerationCallSpec) string {
	return relaycommon.ImageGenerationCallKey(spec.Quality, spec.Size)
}

func completedImageGenerationCallSpec(completed responsesImageGenerationCallSpec, observed responsesImageGenerationCallSpec) responsesImageGenerationCallSpec {
	if completed.ID == "" {
		completed.ID = observed.ID
	}
	if completed.Quality == "" {
		completed.Quality = observed.Quality
	}
	if completed.Size == "" {
		completed.Size = observed.Size
	}
	return completed
}

func responsesImageGenerationCallTrackerFor(c *gin.Context, key string) *responsesImageGenerationCallTracker {
	if c == nil {
		return nil
	}
	if tracker := responsesImageGenerationCallTrackerFromContext(c, key); tracker != nil {
		return tracker
	}
	tracker := &responsesImageGenerationCallTracker{
		ByID:         make(map[string][]responsesImageGenerationCallSpec),
		BySpec:       make(map[string]int),
		ConsumedByID: make(map[string][]responsesImageGenerationCallSpec),
	}
	c.Set(key, tracker)
	return tracker
}

func responsesImageGenerationCallTrackerFromContext(c *gin.Context, key string) *responsesImageGenerationCallTracker {
	if c == nil {
		return nil
	}
	if value, exists := c.Get(key); exists {
		if tracker, ok := value.(*responsesImageGenerationCallTracker); ok && tracker != nil {
			return tracker
		}
	}
	return nil
}

func addResponsesImageGenerationCall(c *gin.Context, key string, spec responsesImageGenerationCallSpec) {
	tracker := responsesImageGenerationCallTrackerFor(c, key)
	if tracker == nil {
		return
	}
	if spec.ID != "" {
		tracker.ByID[spec.ID] = append(tracker.ByID[spec.ID], spec)
	}
	tracker.BySpec[imageGenerationSpecKey(spec)]++
}

func hasResponsesImageGenerationCallID(c *gin.Context, key string, id string) bool {
	if id == "" {
		return false
	}
	tracker := responsesImageGenerationCallTrackerFromContext(c, key)
	if tracker == nil {
		return false
	}
	return len(tracker.ByID[id]) > 0
}

func consumeResponsesImageGenerationCall(c *gin.Context, key string, spec responsesImageGenerationCallSpec) (responsesImageGenerationCallSpec, bool) {
	tracker := responsesImageGenerationCallTrackerFor(c, key)
	if tracker == nil {
		return responsesImageGenerationCallSpec{}, false
	}
	if spec.ID != "" {
		if specs := tracker.ByID[spec.ID]; len(specs) > 0 {
			index := selectResponsesImageGenerationSpecIndex(specs, spec)
			observed := specs[index]
			consumeResponsesImageGenerationIDAt(tracker, spec.ID, index, true)
			return observed, true
		}
		if consumedSpec, ok := lookupConsumedResponsesImageGenerationID(tracker, spec.ID, spec); ok {
			return consumedSpec, true
		}
	}
	specKey := imageGenerationSpecKey(spec)
	if tracker.BySpec[specKey] > 0 {
		if observed, ok := consumeResponsesImageGenerationIDForSpec(tracker, specKey); ok {
			return observed, true
		}
		tracker.BySpec[specKey]--
		if tracker.BySpec[specKey] == 0 {
			delete(tracker.BySpec, specKey)
		}
		return spec, true
	}
	if spec.ID != "" && spec.Quality == "" && spec.Size == "" {
		if observed, ok := consumeResponsesImageGenerationOnlyKnownSpec(tracker, spec.ID); ok {
			return observed, true
		}
	}
	if spec.ID == "" && (spec.Quality != "" || spec.Size != "") {
		if observed, ok := consumeResponsesImageGenerationEmptySpecForKnownSpec(tracker, spec); ok {
			return observed, true
		}
	}
	return responsesImageGenerationCallSpec{}, false
}

func consumeResponsesImageGenerationOnlyKnownSpec(tracker *responsesImageGenerationCallTracker, bindID string) (responsesImageGenerationCallSpec, bool) {
	if tracker == nil {
		return responsesImageGenerationCallSpec{}, false
	}
	for _, specs := range tracker.ByID {
		for _, candidate := range specs {
			if candidate.Quality == "" && candidate.Size == "" {
				continue
			}
			return responsesImageGenerationCallSpec{}, false
		}
	}
	var matchedSpec responsesImageGenerationCallSpec
	for specKey, count := range tracker.BySpec {
		if count <= 0 || specKey == relaycommon.ImageGenerationCallKey("", "") {
			continue
		}
		if count > 1 || matchedSpec.Quality != "" || matchedSpec.Size != "" {
			return responsesImageGenerationCallSpec{}, false
		}
		quality, size := relaycommon.SplitImageGenerationCallKey(specKey)
		matchedSpec = responsesImageGenerationCallSpec{Quality: quality, Size: size}
	}
	if matchedSpec.Quality != "" || matchedSpec.Size != "" {
		specKey := imageGenerationSpecKey(matchedSpec)
		tracker.BySpec[specKey]--
		if tracker.BySpec[specKey] == 0 {
			delete(tracker.BySpec, specKey)
		}
		if bindID != "" {
			tracker.ConsumedByID[bindID] = append(tracker.ConsumedByID[bindID], matchedSpec)
		}
		return matchedSpec, true
	}
	return responsesImageGenerationCallSpec{}, false
}

func consumeResponsesImageGenerationIDAt(tracker *responsesImageGenerationCallTracker, id string, index int, remember bool) responsesImageGenerationCallSpec {
	specs := tracker.ByID[id]
	observed := specs[index]
	specs = append(specs[:index], specs[index+1:]...)
	if len(specs) == 0 {
		delete(tracker.ByID, id)
	} else {
		tracker.ByID[id] = specs
	}
	decrementResponsesImageGenerationSpec(tracker, observed)
	if remember {
		tracker.ConsumedByID[id] = append(tracker.ConsumedByID[id], observed)
	}
	return observed
}

func decrementResponsesImageGenerationSpec(tracker *responsesImageGenerationCallTracker, spec responsesImageGenerationCallSpec) {
	if tracker == nil {
		return
	}
	specKey := imageGenerationSpecKey(spec)
	if tracker.BySpec[specKey] <= 0 {
		return
	}
	tracker.BySpec[specKey]--
	if tracker.BySpec[specKey] == 0 {
		delete(tracker.BySpec, specKey)
	}
}

func consumeResponsesImageGenerationIDForSpec(tracker *responsesImageGenerationCallTracker, specKey string) (responsesImageGenerationCallSpec, bool) {
	if tracker == nil || specKey == "" {
		return responsesImageGenerationCallSpec{}, false
	}
	for id, specs := range tracker.ByID {
		for index, candidate := range specs {
			if imageGenerationSpecKey(candidate) != specKey {
				continue
			}
			return consumeResponsesImageGenerationIDAt(tracker, id, index, true), true
		}
	}
	return responsesImageGenerationCallSpec{}, false
}

func consumeResponsesImageGenerationEmptySpecForKnownSpec(tracker *responsesImageGenerationCallTracker, knownSpec responsesImageGenerationCallSpec) (responsesImageGenerationCallSpec, bool) {
	if tracker == nil {
		return responsesImageGenerationCallSpec{}, false
	}
	matchedID := ""
	matchedIndex := -1
	matchedCount := 0
	for id, specs := range tracker.ByID {
		for index, candidate := range specs {
			if candidate.Quality != "" || candidate.Size != "" {
				continue
			}
			matchedID = id
			matchedIndex = index
			matchedCount++
		}
	}
	if matchedCount == 1 {
		observed := consumeResponsesImageGenerationIDAt(tracker, matchedID, matchedIndex, false)
		remembered := observed
		remembered.Quality = knownSpec.Quality
		remembered.Size = knownSpec.Size
		tracker.ConsumedByID[matchedID] = append(tracker.ConsumedByID[matchedID], remembered)
		return observed, true
	}
	return responsesImageGenerationCallSpec{}, false
}

func lookupConsumedResponsesImageGenerationID(tracker *responsesImageGenerationCallTracker, id string, spec responsesImageGenerationCallSpec) (responsesImageGenerationCallSpec, bool) {
	if tracker == nil || id == "" {
		return responsesImageGenerationCallSpec{}, false
	}
	specs := tracker.ConsumedByID[id]
	if len(specs) == 0 {
		return responsesImageGenerationCallSpec{}, false
	}
	// Responses output item IDs are unique within a response. Keep consumed IDs as
	// a dedupe set; callers treat a match as already accounted and skip billing.
	index := selectResponsesImageGenerationSpecIndex(specs, spec)
	return specs[index], true
}

func selectResponsesImageGenerationSpecIndex(specs []responsesImageGenerationCallSpec, spec responsesImageGenerationCallSpec) int {
	if len(specs) <= 1 {
		return 0
	}
	if spec.Quality != "" || spec.Size != "" {
		specKey := imageGenerationSpecKey(spec)
		for index, candidate := range specs {
			if imageGenerationSpecKey(candidate) == specKey {
				return index
			}
		}
		for index, candidate := range specs {
			if candidate.Quality == "" && candidate.Size == "" {
				return index
			}
		}
	}
	return 0
}

func drainResponsesImageGenerationCalls(c *gin.Context, key string) []responsesImageGenerationCallSpec {
	tracker := responsesImageGenerationCallTrackerFromContext(c, key)
	if tracker == nil {
		return nil
	}
	var drained []responsesImageGenerationCallSpec
	byIDSpecCounts := make(map[string]int)
	for _, specs := range tracker.ByID {
		for _, spec := range specs {
			drained = append(drained, spec)
			byIDSpecCounts[imageGenerationSpecKey(spec)]++
		}
	}
	for specKey, count := range tracker.BySpec {
		count -= byIDSpecCounts[specKey]
		if count <= 0 {
			continue
		}
		quality, size := relaycommon.SplitImageGenerationCallKey(specKey)
		for range count {
			drained = append(drained, responsesImageGenerationCallSpec{
				Quality: quality,
				Size:    size,
			})
		}
	}
	c.Set(key, &responsesImageGenerationCallTracker{
		ByID:         make(map[string][]responsesImageGenerationCallSpec),
		BySpec:       make(map[string]int),
		ConsumedByID: make(map[string][]responsesImageGenerationCallSpec),
	})
	return drained
}

func reconcileImageGenerationCallSpec(info *relaycommon.RelayInfo, previous responsesImageGenerationCallSpec, next responsesImageGenerationCallSpec) bool {
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return false
	}
	if next.Quality == "" && next.Size == "" {
		return false
	}
	previousKey := imageGenerationSpecKey(previous)
	nextKey := imageGenerationSpecKey(next)
	if previousKey == nextKey {
		return true
	}
	tool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	if tool == nil || tool.ImageCalls == nil || tool.ImageCalls[previousKey] <= 0 {
		return false
	}
	tool.ImageCalls[previousKey]--
	if tool.ImageCalls[previousKey] == 0 {
		delete(tool.ImageCalls, previousKey)
	}
	tool.ImageCalls[nextKey]++
	if tool.Quality == previous.Quality && tool.Size == previous.Size {
		tool.Quality = next.Quality
		tool.Size = next.Size
	}
	return true
}

func decrementResponsesBuiltInToolCall(info *relaycommon.RelayInfo, toolType string, count int) {
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil || count <= 0 {
		return
	}
	tool := info.ResponsesUsageInfo.BuiltInTools[toolType]
	if tool == nil {
		return
	}
	if tool.CallCount <= count {
		tool.CallCount = 0
		return
	}
	tool.CallCount -= count
}

func decrementResponsesImageGenerationCalls(info *relaycommon.RelayInfo, specs []responsesImageGenerationCallSpec) {
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil || len(specs) == 0 {
		return
	}
	tool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration]
	if tool == nil {
		return
	}
	for _, spec := range specs {
		if tool.CallCount > 0 {
			tool.CallCount--
		}
		if tool.ImageCalls == nil {
			continue
		}
		specKey := imageGenerationSpecKey(spec)
		if tool.ImageCalls[specKey] <= 0 {
			continue
		}
		tool.ImageCalls[specKey]--
		if tool.ImageCalls[specKey] == 0 {
			delete(tool.ImageCalls, specKey)
		}
	}
}

func refreshResponsesImageGenerationCallContext(c *gin.Context, previous responsesImageGenerationCallSpec, next responsesImageGenerationCallSpec) {
	if c == nil || !c.GetBool("image_generation_call") || (next.Quality == "" && next.Size == "") {
		return
	}
	currentQuality := c.GetString("image_generation_call_quality")
	currentSize := c.GetString("image_generation_call_size")
	if (currentQuality != "" || currentSize != "") && (currentQuality != previous.Quality || currentSize != previous.Size) {
		return
	}
	c.Set("image_generation_call_quality", next.Quality)
	c.Set("image_generation_call_size", next.Size)
}

func effectiveImageGenerationCallSpec(target *relaycommon.BuildInToolInfo, incoming *relaycommon.BuildInToolInfo) (string, string) {
	if incoming == nil {
		if target == nil {
			return "", ""
		}
		return target.Quality, target.Size
	}
	quality := incoming.Quality
	size := incoming.Size
	if quality == "" && target != nil {
		quality = target.Quality
	}
	if size == "" && target != nil {
		size = target.Size
	}
	return quality, size
}

func recordResponsesBuiltInToolUsage(c *gin.Context, info *relaycommon.RelayInfo, response *dto.OpenAIResponsesResponse, skipCounts responsesOutputToolCallCounts) {
	if info == nil || response == nil {
		return
	}
	for _, tool := range response.Tools {
		toolType := common.Interface2String(tool["type"])
		if !mergeResponsesBuiltInToolInfo(info, toolType, tool) {
			logUnhandledResponsesToolType(c, toolType)
		}
	}
	counts := countResponsesOutputToolCalls(c, info, response, skipCounts)
	reconcileObservedResponsesOutputToolCalls(c, info, response, skipCounts, counts)
	recordResponsesOutputToolCallContext(c, response, counts)
	if len(response.Output) > 0 {
		c.Set(responsesCompletedOutputFinalizedKey, true)
	}
}

func builtInToolValue(tool map[string]any, key string) string {
	if tool == nil {
		return ""
	}
	return common.Interface2String(tool[key])
}

func recordResponsesOutputToolCallContext(c *gin.Context, response *dto.OpenAIResponsesResponse, counts responsesOutputToolCallCounts) {
	if c == nil {
		return
	}
	if len(counts.imageCalls) > 0 {
		for _, spec := range counts.imageCalls {
			addResponsesImageGenerationCall(c, responsesCompletedImageGenerationCallsKey, spec)
		}
		firstSpec := counts.imageCalls[0]
		markResponsesOutputToolCallContext(c, dto.ResponsesOutputTypeImageGenerationCall, firstSpec.Quality, firstSpec.Size)
	}
	if counts.webSearch > 0 {
		addResponsesToolCallCount(c, responsesCompletedWebSearchCallCountKey, counts.webSearch)
		markResponsesOutputToolCallContext(c, dto.BuildInCallWebSearchCall, "", "")
	}
	if counts.fileSearch > 0 {
		addResponsesToolCallCount(c, responsesCompletedFileSearchCallCountKey, counts.fileSearch)
		markResponsesOutputToolCallContext(c, dto.BuildInCallFileSearchCall, "", "")
	}
}

func addResponsesToolCallCount(c *gin.Context, key string, count int) {
	if c == nil || count <= 0 {
		return
	}
	c.Set(key, c.GetInt(key)+count)
}

func consumeResponsesCompletedToolCallCount(c *gin.Context, key string) bool {
	if c == nil {
		return false
	}
	count := c.GetInt(key)
	if count <= 0 {
		return false
	}
	c.Set(key, count-1)
	return true
}

func markResponsesObservedToolCallID(c *gin.Context, key string, id string) bool {
	if c == nil || id == "" {
		return false
	}
	value, _ := c.Get(key)
	ids, _ := value.(map[string]bool)
	if ids == nil {
		ids = make(map[string]bool)
		c.Set(key, ids)
	}
	if ids[id] {
		return true
	}
	ids[id] = true
	return false
}

func observedResponsesOutputToolCallCounts(c *gin.Context) responsesOutputToolCallCounts {
	if c == nil {
		return responsesOutputToolCallCounts{}
	}
	return responsesOutputToolCallCounts{
		webSearch:  c.GetInt(responsesObservedWebSearchCallCountKey),
		fileSearch: c.GetInt(responsesObservedFileSearchCallCountKey),
	}
}

func reconcileObservedResponsesOutputToolCalls(c *gin.Context, info *relaycommon.RelayInfo, response *dto.OpenAIResponsesResponse, observed responsesOutputToolCallCounts, completed responsesOutputToolCallCounts) {
	if response == nil || len(response.Output) == 0 {
		return
	}
	if completed.imageGeneration > 0 {
		decrementResponsesImageGenerationCalls(info, drainResponsesImageGenerationCalls(c, responsesObservedImageGenerationCallsKey))
	}
	if completed.webSearch > 0 && observed.webSearch > completed.webSearch {
		excess := observed.webSearch - completed.webSearch
		decrementResponsesBuiltInToolCall(info, dto.BuildInToolWebSearchPreview, excess)
		c.Set(responsesObservedWebSearchCallCountKey, completed.webSearch)
	}
	if completed.fileSearch > 0 && observed.fileSearch > completed.fileSearch {
		excess := observed.fileSearch - completed.fileSearch
		decrementResponsesBuiltInToolCall(info, dto.BuildInToolFileSearch, excess)
		c.Set(responsesObservedFileSearchCallCountKey, completed.fileSearch)
	}
}

func markResponsesOutputToolCallContext(c *gin.Context, itemType string, quality string, size string) {
	if c == nil {
		return
	}
	switch itemType {
	case dto.ResponsesOutputTypeImageGenerationCall:
		if c.GetBool("image_generation_call") {
			return
		}
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", quality)
		c.Set("image_generation_call_size", size)
	case dto.BuildInCallWebSearchCall:
		c.Set("web_search_call", true)
	case dto.BuildInCallFileSearchCall:
		c.Set("file_search_call", true)
	}
}

func isResponsesCompactionOutputType(itemType string) bool {
	switch itemType {
	case dto.ResponsesOutputTypeCompaction, dto.ResponsesOutputTypeCompactionSummary, dto.ResponsesOutputTypeContextCompaction:
		return true
	default:
		return false
	}
}

func markResponsesCompactionOutputContext(c *gin.Context, item *dto.ResponsesOutput) bool {
	if item == nil || !isResponsesCompactionOutputType(item.Type) {
		return false
	}
	common.SetContextKey(c, constant.ContextKeyResponsesCompactionOutput, true)
	return true
}

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	if info != nil && info.ChannelMeta != nil && !common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry) {
		service.RecordResponsesEncryptedContentAffinityWithStatus(c, responseBody, info.ChannelMeta.ChannelId, resp.StatusCode)
	}

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}
	if responsesResponse.HasCompactionOutput() {
		common.SetContextKey(c, constant.ContextKeyResponsesCompactionOutput, true)
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		applyResponsesUsageDetails(&usage, responsesResponse.Usage, true)
	}
	if info == nil {
		return &usage, nil
	}
	recordResponsesBuiltInToolUsage(c, info, &responsesResponse, responsesOutputToolCallCounts{})
	return &usage, nil
}

func applyResponsesUsageDetails(target *dto.Usage, source *dto.Usage, overwriteZero bool) {
	if target == nil || source == nil {
		return
	}
	if overwriteZero || source.InputTokens != 0 {
		target.PromptTokens = source.InputTokens
	}
	if overwriteZero || source.OutputTokens != 0 {
		target.CompletionTokens = source.OutputTokens
	}
	if overwriteZero || source.TotalTokens != 0 {
		target.TotalTokens = source.TotalTokens
	}
	if source.InputTokensDetails != nil {
		target.PromptTokensDetails.CachedTokens = source.InputTokensDetails.CachedTokens
		target.PromptTokensDetails.ImageTokens = source.InputTokensDetails.ImageTokens
		target.PromptTokensDetails.AudioTokens = source.InputTokensDetails.AudioTokens
	}
	outputDetails, hasOutputDetails := responsesUsageOutputDetails(source, overwriteZero)
	if !hasOutputDetails {
		return
	}
	if overwriteZero || outputDetails.ReasoningTokens != 0 {
		target.CompletionTokenDetails.ReasoningTokens = outputDetails.ReasoningTokens
	}
	if overwriteZero || outputDetails.ImageTokens != 0 {
		target.CompletionTokenDetails.ImageTokens = outputDetails.ImageTokens
	}
	if overwriteZero || outputDetails.AudioTokens != 0 {
		target.CompletionTokenDetails.AudioTokens = outputDetails.AudioTokens
	}
}

func responsesUsageOutputDetails(source *dto.Usage, overwriteZero bool) (dto.OutputTokenDetails, bool) {
	if source == nil {
		return dto.OutputTokenDetails{}, false
	}
	if source.OutputTokensDetails != nil {
		return *source.OutputTokensDetails, true
	}
	details := source.CompletionTokenDetails
	if overwriteZero || details.ReasoningTokens != 0 || details.ImageTokens != 0 || details.AudioTokens != 0 {
		return details, true
	}
	return dto.OutputTokenDetails{}, false
}

func countResponsesOutputToolCalls(c *gin.Context, info *relaycommon.RelayInfo, response *dto.OpenAIResponsesResponse, skipCounts responsesOutputToolCallCounts) responsesOutputToolCallCounts {
	var counts responsesOutputToolCallCounts
	if response == nil {
		return counts
	}
	for _, output := range response.Output {
		switch output.Type {
		case dto.ResponsesOutputTypeImageGenerationCall:
			spec := imageGenerationCallSpecFromOutput(&output)
			if observedSpec, ok := consumeResponsesImageGenerationCall(c, responsesObservedImageGenerationCallsKey, spec); ok {
				if reconcileImageGenerationCallSpec(info, observedSpec, spec) {
					refreshResponsesImageGenerationCallContext(c, observedSpec, spec)
				}
				counts.imageGeneration++
				counts.imageCalls = append(counts.imageCalls, completedImageGenerationCallSpec(spec, observedSpec))
				continue
			}
			effectiveSpec, ok := countResponsesBuiltInToolCallWithSpec(info, dto.BuildInToolImageGeneration, map[string]any{
				"id":      output.ID,
				"quality": output.Quality,
				"size":    output.Size,
			})
			if ok {
				counts.imageGeneration++
				counts.imageCalls = append(counts.imageCalls, effectiveSpec)
			}
		case dto.BuildInCallWebSearchCall:
			counts.webSearch++
			if skipCounts.webSearch > 0 {
				skipCounts.webSearch--
				continue
			}
			countResponsesBuiltInToolCall(info, dto.BuildInToolWebSearchPreview, nil)
		case dto.BuildInCallFileSearchCall:
			counts.fileSearch++
			if skipCounts.fileSearch > 0 {
				skipCounts.fileSearch--
				continue
			}
			countResponsesBuiltInToolCall(info, dto.BuildInToolFileSearch, nil)
		}
	}
	return counts
}

func countResponsesOutputItemDone(c *gin.Context, info *relaycommon.RelayInfo, item *dto.ResponsesOutput) {
	if item == nil {
		return
	}
	switch item.Type {
	case dto.ResponsesOutputTypeImageGenerationCall:
		spec := imageGenerationCallSpecFromOutput(item)
		if completedSpec, ok := consumeResponsesImageGenerationCall(c, responsesCompletedImageGenerationCallsKey, spec); ok {
			if reconcileImageGenerationCallSpec(info, completedSpec, spec) {
				refreshResponsesImageGenerationCallContext(c, completedSpec, spec)
			}
			return
		}
		if c.GetBool(responsesCompletedOutputFinalizedKey) {
			return
		}
		if hasResponsesImageGenerationCallID(c, responsesObservedImageGenerationCallsKey, spec.ID) {
			return
		}
		effectiveSpec, ok := countResponsesBuiltInToolCallWithSpec(info, dto.BuildInToolImageGeneration, map[string]any{
			"id":      item.ID,
			"quality": item.Quality,
			"size":    item.Size,
		})
		if ok {
			addResponsesImageGenerationCall(c, responsesObservedImageGenerationCallsKey, effectiveSpec)
		}
		markResponsesOutputToolCallContext(c, item.Type, item.Quality, item.Size)
	case dto.BuildInCallWebSearchCall:
		if consumeResponsesCompletedToolCallCount(c, responsesCompletedWebSearchCallCountKey) {
			return
		}
		if c.GetBool(responsesCompletedOutputFinalizedKey) {
			return
		}
		if markResponsesObservedToolCallID(c, responsesObservedWebSearchCallIDsKey, strings.TrimSpace(item.ID)) {
			return
		}
		countResponsesBuiltInToolCall(info, dto.BuildInToolWebSearchPreview, nil)
		addResponsesToolCallCount(c, responsesObservedWebSearchCallCountKey, 1)
		markResponsesOutputToolCallContext(c, item.Type, "", "")
	case dto.BuildInCallFileSearchCall:
		if consumeResponsesCompletedToolCallCount(c, responsesCompletedFileSearchCallCountKey) {
			return
		}
		if c.GetBool(responsesCompletedOutputFinalizedKey) {
			return
		}
		if markResponsesObservedToolCallID(c, responsesObservedFileSearchCallIDsKey, strings.TrimSpace(item.ID)) {
			return
		}
		countResponsesBuiltInToolCall(info, dto.BuildInToolFileSearch, nil)
		addResponsesToolCallCount(c, responsesObservedFileSearchCallCountKey, 1)
		markResponsesOutputToolCallContext(c, item.Type, "", "")
	}
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	var fallbackTextTruncated bool
	hasCompletedUsage := false

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		sendResponsesStreamData(c, streamResponse, data)
		switch streamResponse.Type {
		case "response.completed":
			if streamResponse.Response != nil {
				if streamResponse.Response.Usage != nil {
					hasCompletedUsage = true
					applyResponsesUsageDetails(usage, streamResponse.Response.Usage, true)
				}
				recordResponsesBuiltInToolUsage(c, info, streamResponse.Response, observedResponsesOutputToolCallCounts(c))
				if streamResponse.Response.HasImageGenerationCall() {
					if !c.GetBool("image_generation_call") {
						c.Set("image_generation_call", true)
						c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
						c.Set("image_generation_call_size", streamResponse.Response.GetSize())
					}
				}
				if streamResponse.Response.HasCompactionOutput() {
					common.SetContextKey(c, constant.ContextKeyResponsesCompactionOutput, true)
				}
				if info != nil && info.ChannelMeta != nil && !common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry) {
					responseBody, err := common.Marshal(streamResponse.Response)
					if err == nil {
						service.RecordResponsesEncryptedContentAffinityWithStatus(c, responseBody, info.ChannelMeta.ChannelId, resp.StatusCode)
					}
				}
			}
			sr.Done()
		case "response.output_text.delta":
			// 处理输出文本
			if appendResponsesFallbackText(&responseTextBuilder, streamResponse.Delta) {
				fallbackTextTruncated = true
			}
		case "response.reasoning_summary_text.delta":
			if appendResponsesFallbackText(&responseTextBuilder, streamResponse.Delta) {
				fallbackTextTruncated = true
			}
		case dto.ResponsesOutputTypeItemDone:
			// 函数调用处理
			if streamResponse.Item != nil {
				if !markResponsesCompactionOutputContext(c, streamResponse.Item) {
					countResponsesOutputItemDone(c, info, streamResponse.Item)
				}
			}
		}
	})

	if newAPIError := retryableUpstreamStreamInterruptedError(c, info); newAPIError != nil {
		return nil, newAPIError
	}

	shouldEstimateCompletionTokens := usage.CompletionTokens == 0 &&
		(!hasCompletedUsage || responseTextBuilder.Len() > 0)
	if shouldEstimateCompletionTokens {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
			if fallbackTextTruncated {
				logger.LogWarn(c, fmt.Sprintf("responses stream fallback text truncated before token estimation: max_bytes=%d", responsesFallbackTextMaxBytes))
			}
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return usage, nil
}

func newBuiltInToolInfo(toolType string, tool map[string]any) *relaycommon.BuildInToolInfo {
	switch toolType {
	case dto.BuildInToolImageGeneration:
		return &relaycommon.BuildInToolInfo{
			ToolName: toolType,
			Quality:  builtInToolValue(tool, "quality"),
			Size:     builtInToolValue(tool, "size"),
		}
	case dto.BuildInToolWebSearchPreview:
		searchContextSize := builtInToolValue(tool, "search_context_size")
		defaultSearchSize := searchContextSize == ""
		if searchContextSize == "" {
			searchContextSize = "medium"
		}
		return &relaycommon.BuildInToolInfo{
			ToolName:          toolType,
			SearchContextSize: searchContextSize,
			DefaultSearchSize: defaultSearchSize,
		}
	case dto.BuildInToolFileSearch:
		return &relaycommon.BuildInToolInfo{
			ToolName: toolType,
		}
	}
	return nil
}
