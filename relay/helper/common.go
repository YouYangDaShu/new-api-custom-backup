package helper

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func FlushWriter(c *gin.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("flush panic recovered: %v", r)
		}
	}()

	if c == nil || c.Writer == nil {
		return nil
	}
	if streamDeliverySuppressed(c) {
		return nil
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}

	flusher.Flush()
	return nil
}

func requestContextDone(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.Request.Context().Err() != nil
}

func SetEventStreamHeaders(c *gin.Context) {
	// 检查是否已经设置过头部
	if _, exists := c.Get("event_stream_headers_set"); exists {
		return
	}

	// 设置标志，表示头部已经设置过
	c.Set("event_stream_headers_set", true)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}

func ClaudeData(c *gin.Context, resp dto.ClaudeResponse) error {
	if streamDeliverySuppressed(c) {
		return nil
	}
	if requestContextDone(c) {
		return nil
	}

	jsonData, err := common.Marshal(resp)
	if err != nil {
		common.SysError("error marshalling stream response: " + err.Error())
	} else {
		c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
		c.Render(-1, common.CustomEvent{Data: "data: " + string(jsonData)})
		if err := FlushWriter(c); err == nil && HasClaudeModelPayload(&resp) {
			MarkModelPayloadDelivered(c)
		}
		return nil
	}
	_ = FlushWriter(c)
	return nil
}

func ClaudeChunkData(c *gin.Context, resp dto.ClaudeResponse, data string) {
	if streamDeliverySuppressed(c) {
		return
	}
	if requestContextDone(c) {
		return
	}

	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("data: %s\n", data)})
	if err := FlushWriter(c); err == nil && data != "" && data != "[DONE]" && HasClaudeModelPayload(&resp) {
		MarkModelPayloadDelivered(c)
	}
}

func ResponseChunkData(c *gin.Context, resp dto.ResponsesStreamResponse, data string) error {
	if streamDeliverySuppressed(c) {
		return nil
	}
	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
	c.Render(-1, common.CustomEvent{Data: fmt.Sprintf("data: %s", data)})
	return FlushWriter(c)
}

func StringData(c *gin.Context, str string) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}
	if streamDeliverySuppressed(c) {
		return nil
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	c.Render(-1, common.CustomEvent{Data: "data: " + str})
	return FlushWriter(c)
}

func MarkModelPayloadDelivered(c *gin.Context) {
	if c != nil && !streamDeliverySuppressed(c) && !requestContextDone(c) {
		common.SetContextKey(c, constant.ContextKeyModelPayloadDelivered, true)
	}
}

// HasChatCompletionsModelPayload reports whether a stream chunk contains
// replay-sensitive model output rather than protocol metadata.
func HasChatCompletionsModelPayload(resp *dto.ChatCompletionsStreamResponse) bool {
	if resp == nil {
		return false
	}
	for _, choice := range resp.Choices {
		delta := choice.Delta
		if delta.Content != nil && *delta.Content != "" {
			return true
		}
		if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
			return true
		}
		if delta.Reasoning != nil && *delta.Reasoning != "" {
			return true
		}
		for _, tool := range delta.ToolCalls {
			if tool.ID != "" || tool.Function.Name != "" || tool.Function.Arguments != "" || tool.Type != nil {
				return true
			}
		}
	}
	return false
}

// HasClaudeModelPayload reports whether a Claude event carries visible model
// content. Lifecycle events such as message_start, ping, and message_stop do
// not qualify solely because they contain usage or status fields.
func HasClaudeModelPayload(resp *dto.ClaudeResponse) bool {
	if resp == nil {
		return false
	}
	switch resp.Type {
	case "content_block_delta":
		return claudeMediaHasPayload(resp.Delta)
	case "content_block_start":
		return claudeMediaHasPayload(resp.ContentBlock)
	case "message_start", "message_delta", "message_stop", "content_block_stop", "ping":
		return false
	}
	if resp.Completion != "" || claudeMediaSliceHasPayload(resp.Content) || claudeMediaHasPayload(resp.Delta) || claudeMediaHasPayload(resp.ContentBlock) {
		return true
	}
	return false
}

func claudeMediaSliceHasPayload(items []dto.ClaudeMediaMessage) bool {
	for i := range items {
		if claudeMediaHasPayload(&items[i]) {
			return true
		}
	}
	return false
}

func claudeMediaHasPayload(media *dto.ClaudeMediaMessage) bool {
	if media == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(media.Type)) {
	case "tool_use", "server_tool_use", "image":
		return true
	}
	if (media.Text != nil && *media.Text != "") || (media.Thinking != nil && *media.Thinking != "") ||
		(media.PartialJson != nil && *media.PartialJson != "") || media.Delta != "" || media.Id != "" || media.Name != "" ||
		media.Input != nil || media.Source != nil || media.Content != nil {
		return true
	}
	return false
}

// HasResponsesModelPayload reports whether a Responses event carries visible
// text, reasoning, tool arguments, or an output item with actual content.
func HasResponsesModelPayload(resp *dto.ResponsesStreamResponse) bool {
	if resp == nil {
		return false
	}
	if resp.Delta != "" || (resp.Part != nil && strings.TrimSpace(resp.Part.Text) != "") {
		return true
	}
	if responsesItemHasPayload(resp.Item) {
		return true
	}
	if resp.Response != nil {
		for _, item := range resp.Response.Output {
			if responsesItemHasPayload(&item) {
				return true
			}
		}
	}
	return false
}

func responsesItemHasPayload(item *dto.ResponsesOutput) bool {
	if item == nil {
		return false
	}
	if strings.TrimSpace(item.Name) != "" || len(item.Arguments) > 0 || strings.TrimSpace(item.CallId) != "" {
		return true
	}
	for _, content := range item.Content {
		if strings.TrimSpace(content.Text) != "" {
			return true
		}
	}
	switch item.Type {
	case dto.ResponsesOutputTypeImageGenerationCall, dto.BuildInCallWebSearchCall:
		return true
	}
	return false
}

func PingData(c *gin.Context) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}
	if streamDeliverySuppressed(c) {
		return nil
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	if _, err := c.Writer.Write([]byte(": PING\n\n")); err != nil {
		return fmt.Errorf("write ping data failed: %w", err)
	}
	return FlushWriter(c)
}

func ObjectData(c *gin.Context, object interface{}) error {
	if object == nil {
		return errors.New("object is nil")
	}
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	return StringData(c, string(jsonData))
}

func Done(c *gin.Context) {
	_ = StringData(c, "[DONE]")
}

func WssString(c *gin.Context, ws *websocket.Conn, str string) error {
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", str))
	return ws.WriteMessage(1, []byte(str))
}

func WssObject(c *gin.Context, ws *websocket.Conn, object interface{}) error {
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", jsonData))
	return ws.WriteMessage(1, jsonData)
}

func WssError(c *gin.Context, ws *websocket.Conn, openaiError types.OpenAIError) {
	if ws == nil {
		return
	}
	errorObj := &dto.RealtimeEvent{
		Type:    "error",
		EventId: GetLocalRealtimeID(c),
		Error:   &openaiError,
	}
	_ = WssObject(c, ws, errorObj)
}

func GetResponseID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("chatcmpl-%s", logID)
}

func GetLocalRealtimeID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("evt_%s", logID)
}

func GenerateStartEmptyResponse(id string, createAt int64, model string, systemFingerprint *string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role:    "assistant",
					Content: common.GetPointer(""),
				},
			},
		},
	}
}

func GenerateStopResponse(id string, createAt int64, model string, finishReason string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				FinishReason: &finishReason,
			},
		},
	}
}

func GenerateFinalUsageResponse(id string, createAt int64, model string, usage dto.Usage) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices:           make([]dto.ChatCompletionsStreamResponseChoice, 0),
		Usage:             &usage,
	}
}
