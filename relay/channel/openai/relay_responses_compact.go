package openai

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesCompactionHandler(c *gin.Context, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var compactResp dto.OpenAIResponsesCompactionResponse
	if err := common.Unmarshal(responseBody, &compactResp); err != nil {
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		return nil, types.NewOpenAIError(err, types.ErrorCodeChannelBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := compactResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		if compactResp.Usage != nil {
			common.SetContextKey(c, constant.ContextKeyUpstreamUsagePresent, true)
			common.SetContextKey(c, constant.ContextKeyErrorUsagePresent, true)
			return usageFromCompactionUsage(compactResp.Usage), types.WithOpenAIError(*oaiError, resp.StatusCode, types.ErrOptionWithSkipRetry())
		}
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode, types.ErrOptionWithSkipRetry())
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	if compactResp.Usage == nil {
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		return nil, nil
	}
	common.SetContextKey(c, constant.ContextKeyUpstreamUsagePresent, true)
	common.SetContextKey(c, constant.ContextKeyErrorUsagePresent, false)
	return usageFromCompactionUsage(compactResp.Usage), nil
}

func usageFromCompactionUsage(src *dto.Usage) *dto.Usage {
	usage := &dto.Usage{PromptTokens: src.InputTokens, CompletionTokens: src.OutputTokens, TotalTokens: src.TotalTokens}
	if src.InputTokensDetails != nil {
		usage.PromptTokensDetails.CachedTokens = src.InputTokensDetails.CachedTokens
	}
	return usage
}
