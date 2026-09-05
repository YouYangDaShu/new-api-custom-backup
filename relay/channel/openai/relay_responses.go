package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

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

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	status := responsesStatus(&responsesResponse)
	common.SetContextKey(c, constant.ContextKeyUpstreamTerminalStatus, status)
	if terminalErr := responsesTerminalError(&responsesResponse, status, resp.StatusCode); terminalErr != nil {
		return responsesErrorUsage(c, &responsesResponse, terminalErr)
	}

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		common.SetContextKey(c, constant.ContextKeyUpstreamUsagePresent, true)
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
		}
	} else {
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
	}
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// 解析 Tools 用量
	for _, tool := range responsesResponse.Tools {
		buildToolinfo, ok := info.ResponsesUsageInfo.BuiltInTools[common.Interface2String(tool["type"])]
		if !ok || buildToolinfo == nil {
			logger.LogError(c, fmt.Sprintf("BuiltInTools not found for tool type: %v", tool["type"]))
			continue
		}
		buildToolinfo.CallCount++
	}
	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	containUsage := false
	terminalStatus := ""
	var terminalErr *types.NewAPIError
	var responseTextBuilder strings.Builder

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		if streamResponse.Response != nil && streamResponse.Response.Usage != nil {
			common.SetContextKey(c, constant.ContextKeyUpstreamUsagePresent, true)
			containUsage = true
			usage.PromptTokens = streamResponse.Response.Usage.InputTokens
			usage.CompletionTokens = streamResponse.Response.Usage.OutputTokens
			usage.TotalTokens = streamResponse.Response.Usage.TotalTokens
			if streamResponse.Response.Usage.InputTokensDetails != nil {
				usage.PromptTokensDetails.CachedTokens = streamResponse.Response.Usage.InputTokensDetails.CachedTokens
			}
		}
		if failureErr := responsesFailureError(streamResponse.Response, streamResponse.Type, http.StatusInternalServerError); failureErr != nil {
			terminalStatus = streamResponse.Type
			terminalErr = failureErr
			common.SetContextKey(c, constant.ContextKeyUpstreamTerminalStatus, streamResponse.Type)
			sendResponsesStreamData(c, streamResponse, data)
			sr.Stop(failureErr)
			return
		}
		sendResponsesStreamData(c, streamResponse, data)
		switch streamResponse.Type {
		case "response.completed":
			terminalStatus = streamResponse.Type
			common.SetContextKey(c, constant.ContextKeyUpstreamTerminalStatus, streamResponse.Type)
			if streamResponse.Response != nil {
				if streamResponse.Response.HasImageGenerationCall() {
					c.Set("image_generation_call", true)
					c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
					c.Set("image_generation_call_size", streamResponse.Response.GetSize())
				}
			}
		case "response.output_text.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			// 函数调用处理
			if streamResponse.Item != nil {
				switch streamResponse.Item.Type {
				case dto.BuildInCallWebSearchCall:
					if info != nil && info.ResponsesUsageInfo != nil && info.ResponsesUsageInfo.BuiltInTools != nil {
						if webSearchTool, exists := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool != nil {
							webSearchTool.CallCount++
						}
					}
				}
			}
		case "response.done":
			terminalStatus = streamResponse.Type
			common.SetContextKey(c, constant.ContextKeyUpstreamTerminalStatus, streamResponse.Type)
		}
	})
	reason := relaycommon.StreamEndReasonNone
	var endErr error
	streamHasErrors := false
	if info != nil && info.StreamStatus != nil {
		reason, endErr = info.StreamStatus.Snapshot()
		streamHasErrors = info.StreamStatus.HasErrors()
	}
	if terminalErr != nil || reason == relaycommon.StreamEndReasonHandlerStop || endErr != nil || streamHasErrors || terminalStatus == "" {
		if terminalErr == nil {
			if endErr == nil {
				if terminalStatus == "" {
					endErr = fmt.Errorf("responses stream ended without terminal event (%s)", reason)
				} else {
					endErr = fmt.Errorf("responses stream ended abnormally (%s)", reason)
				}
			}
			terminalErr = newResponsesErrorWithEvidence(endErr, types.ErrorCodeBadResponse, http.StatusInternalServerError,
				responseDeliveryObserved(c) || containUsage || terminalStatus != "")
		}
		if !containUsage {
			common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
			return nil, terminalErr
		}
		common.SetContextKey(c, constant.ContextKeyUpstreamUsagePresent, true)
		common.SetContextKey(c, constant.ContextKeyErrorUsagePresent, true)
		return usage, terminalErr
	}

	if !containUsage {
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
	} else {
		common.SetContextKey(c, constant.ContextKeyUpstreamUsagePresent, true)
	}

	return usage, nil
}

func responseDeliveryObserved(c *gin.Context) bool {
	return common.GetContextKeyBool(c, constant.ContextKeyModelPayloadDelivered)
}

// newResponsesErrorWithEvidence keeps ordinary post-200 parse failures
// retryable until the relay has evidence that replaying could duplicate a
// response. Explicit terminal errors are already no-retry and are preserved.
func newResponsesErrorWithEvidence(err error, errorCode types.ErrorCode, statusCode int, evidence bool) *types.NewAPIError {
	if err == nil {
		err = fmt.Errorf("responses relay failed")
	}
	if existing, ok := err.(*types.NewAPIError); ok && types.IsSkipRetryError(existing) {
		return existing
	}
	cleanErr := fmt.Errorf("%s", err.Error())
	if evidence {
		return types.NewOpenAIError(cleanErr, errorCode, statusCode, types.ErrOptionWithSkipRetry())
	}
	return types.NewOpenAIError(cleanErr, errorCode, statusCode)
}
