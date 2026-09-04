package openai

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/relayconvert"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesToChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var responsesResp dto.OpenAIResponsesResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	if err := common.Unmarshal(body, &responsesResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := responsesResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	chatId := helper.GetResponseID(c)
	chatResp, usage, err := service.ResponsesResponseToChatCompletionsResponse(&responsesResp, chatId)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if responsesResp.Usage == nil {
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		usage = &dto.Usage{}
		chatResp.Usage = *usage
	}

	var responseBody []byte
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		claudeResp := service.ResponseOpenAI2Claude(chatResp, info)
		responseBody, err = common.Marshal(claudeResp)
	case types.RelayFormatGemini:
		geminiResp := service.ResponseOpenAI2Gemini(chatResp, info)
		responseBody, err = common.Marshal(geminiResp)
	default:
		responseBody, err = common.Marshal(chatResp)
	}
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiResponsesToChatBufferedStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	accumulator := relayconvert.NewResponsesBufferedAccumulator()
	var finalResponse *dto.OpenAIResponsesResponse
	var streamErr *types.NewAPIError
	terminal := false
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if terminal || streamErr != nil {
			return
		}
		var streamResp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
			logger.LogError(c, "failed to unmarshal buffered responses stream event: "+err.Error())
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return
		}
		accumulator.ProcessEvent(&streamResp)
		switch streamResp.Type {
		case "response.completed", "response.done", "response.incomplete", "response.failed", "response.error", "response.cancelled":
			common.SetContextKey(c, constant.ContextKeyUpstreamTerminalStatus, streamResp.Type)
			finalResponse = streamResp.Response
			if streamResp.Type == "response.incomplete" {
				if finalResponse == nil {
					finalResponse = &dto.OpenAIResponsesResponse{}
				}
				if len(finalResponse.Status) == 0 {
					finalResponse.Status = []byte(`"incomplete"`)
				}
			}
			if streamResp.Type == "response.failed" || streamResp.Type == "response.error" || streamResp.Type == "response.cancelled" {
				if streamResp.Response != nil {
					if oaiErr := streamResp.Response.GetOpenAIError(); oaiErr != nil && oaiErr.Type != "" {
						streamErr = types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
						sr.Stop(streamErr)
						return
					}
				}
				streamErr = types.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResp.Type), types.ErrorCodeBadResponse, http.StatusInternalServerError)
				sr.Stop(streamErr)
				return
			}
			terminal = true
			sr.Done()
		}
	})
	if streamErr == nil && info != nil && info.StreamStatus != nil {
		reason, statusErr := info.StreamStatus.Snapshot()
		if statusErr != nil || (reason != relaycommon.StreamEndReasonDone && terminal == false) {
			if statusErr == nil {
				statusErr = fmt.Errorf("responses buffered stream ended without terminal event (%s)", reason)
			}
			streamErr = types.NewOpenAIError(statusErr, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
	}
	if streamErr != nil {
		if finalResponse != nil && finalResponse.Usage != nil {
			common.SetContextKey(c, constant.ContextKeyBillingSettledOnError, true)
			return relayconvert.UsageFromResponsesUsage(finalResponse.Usage), streamErr
		}
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		return nil, streamErr
	}
	if finalResponse == nil {
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		finalResponse = &dto.OpenAIResponsesResponse{
			ID:        helper.GetResponseID(c),
			CreatedAt: int(time.Now().Unix()),
			Model:     info.UpstreamModelName,
			Status:    []byte(`"completed"`),
		}
	}
	accumulator.SupplementResponseOutput(finalResponse)

	chatId := helper.GetResponseID(c)
	chatResp, usage, err := service.ResponsesResponseToChatCompletionsResponse(finalResponse, chatId)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if finalResponse.Usage == nil {
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		usage = &dto.Usage{}
		chatResp.Usage = *usage
	}

	var responseBody []byte
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		claudeResp := service.ResponseOpenAI2Claude(chatResp, info)
		responseBody, err = common.Marshal(claudeResp)
	case types.RelayFormatGemini:
		geminiResp := service.ResponseOpenAI2Gemini(chatResp, info)
		responseBody, err = common.Marshal(geminiResp)
	default:
		responseBody, err = common.Marshal(chatResp)
	}
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiResponsesToChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	responseId := helper.GetResponseID(c)
	createAt := time.Now().Unix()
	state := relayconvert.NewResponsesToChatStreamState(info.UpstreamModelName, false)
	state.ID = responseId
	state.Created = createAt
	streamErr := (*types.NewAPIError)(nil)

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo == nil {
		info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
	}

	sendChatChunk := func(chunk dto.ChatCompletionsStreamResponse) bool {
		if len(chunk.Choices) == 0 && chunk.Usage == nil {
			return true
		}
		if info.RelayFormat == types.RelayFormatOpenAI {
			if err := helper.ObjectData(c, &chunk); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			return true
		}

		chunkData, err := common.Marshal(&chunk)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		if err := HandleStreamFormat(c, info, string(chunkData), false, false); err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		return true
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		var streamResp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
			logger.LogError(c, "failed to unmarshal responses stream event: "+err.Error())
			sr.Error(err)
			return
		}

		if streamResp.Type == "response.error" || streamResp.Type == "response.failed" {
			if streamResp.Response != nil {
				if oaiErr := streamResp.Response.GetOpenAIError(); oaiErr != nil && oaiErr.Type != "" {
					streamErr = types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
					sr.Stop(streamErr)
					return
				}
			}
			streamErr = types.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResp.Type), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return
		}

		chunks, err := relayconvert.ResponsesStreamEventToChatChunks(&streamResp, state)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			sr.Stop(streamErr)
			return
		}
		for _, chunk := range chunks {
			if !sendChatChunk(chunk) {
				sr.Stop(streamErr)
				return
			}
		}
		if streamResp.Type == "response.incomplete" {
			streamErr = types.NewOpenAIError(fmt.Errorf("responses stream incomplete"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			sr.Stop(streamErr)
		}
	})

	if streamErr != nil {
		common.SetContextKey(c, constant.ContextKeyUpstreamTerminalStatus, state.TerminalStatus)
		if state.UsagePresent {
			usage := state.Usage
			common.SetContextKey(c, constant.ContextKeyBillingSettledOnError, true)
			return usage, streamErr
		}
		if !state.UsagePresent {
			common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		}
		return nil, streamErr
	}

	usage := state.Usage
	if !state.UsagePresent {
		common.SetContextKey(c, constant.ContextKeyMissingUpstreamUsage, true)
		usage = &dto.Usage{}
		state.Usage = usage
	}

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil {
		info.ClaudeConvertInfo.Usage = usage
	}
	for _, chunk := range relayconvert.FinalizeResponsesToChatStream(state) {
		if !sendChatChunk(chunk) {
			return nil, streamErr
		}
	}
	if info.RelayFormat == types.RelayFormatOpenAI && info.ShouldIncludeUsage && usage != nil {
		if err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(responseId, state.Created, state.Model, *usage)); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
	}

	if info.RelayFormat == types.RelayFormatOpenAI {
		helper.Done(c)
	}
	return usage, nil
}
