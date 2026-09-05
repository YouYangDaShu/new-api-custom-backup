package helper

import (
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPayloadSemantics_ChatExcludesProtocolOnlyChunks(t *testing.T) {
	empty := ""
	role := "assistant"
	finish := "stop"
	toolName := "lookup"
	cases := []struct {
		name string
		resp dto.ChatCompletionsStreamResponse
		want bool
	}{
		{name: "role only", resp: dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: role}}}}},
		{name: "empty content", resp: dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &empty}}}}},
		{name: "finish only", resp: dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &finish}}}},
		{name: "usage only", resp: dto.ChatCompletionsStreamResponse{Usage: &dto.Usage{TotalTokens: 3}}},
		{name: "content", resp: dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: func() *string { s := "hello"; return &s }()}}}}, want: true},
		{name: "tool delta", resp: dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{Function: dto.FunctionResponse{Name: toolName}}}}}}}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assert.Equal(t, tc.want, HasChatCompletionsModelPayload(&tc.resp)) })
	}
}

func TestModelPayloadSemantics_ClaudeExcludesLifecycleEvents(t *testing.T) {
	empty := ""
	text := "hello"
	cases := []struct {
		name string
		resp dto.ClaudeResponse
		want bool
	}{
		{name: "message start", resp: dto.ClaudeResponse{Type: "message_start", Message: &dto.ClaudeMediaMessage{Usage: &dto.ClaudeUsage{InputTokens: 2}}}},
		{name: "ping", resp: dto.ClaudeResponse{Type: "ping"}},
		{name: "empty text delta", resp: dto.ClaudeResponse{Type: "content_block_delta", Delta: &dto.ClaudeMediaMessage{Text: &empty}}},
		{name: "text delta", resp: dto.ClaudeResponse{Type: "content_block_delta", Delta: &dto.ClaudeMediaMessage{Text: &text}}, want: true},
		{name: "tool block start", resp: dto.ClaudeResponse{Type: "content_block_start", ContentBlock: &dto.ClaudeMediaMessage{Type: "tool_use"}}, want: true},
		{name: "message stop", resp: dto.ClaudeResponse{Type: "message_stop", Usage: &dto.ClaudeUsage{OutputTokens: 2}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assert.Equal(t, tc.want, HasClaudeModelPayload(&tc.resp)) })
	}
}

func TestModelPayloadSemantics_ResponsesExcludesLifecycleEnvelopes(t *testing.T) {
	cases := []struct {
		name string
		resp dto.ResponsesStreamResponse
		want bool
	}{
		{name: "created", resp: dto.ResponsesStreamResponse{Type: "response.created"}},
		{name: "item added empty", resp: dto.ResponsesStreamResponse{Type: dto.ResponsesOutputTypeItemAdded, Item: &dto.ResponsesOutput{Type: "message"}}},
		{name: "text delta", resp: dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "hello"}, want: true},
		{name: "arguments delta", resp: dto.ResponsesStreamResponse{Type: "response.function_call_arguments.delta", Delta: "{\"q\":1}"}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assert.Equal(t, tc.want, HasResponsesModelPayload(&tc.resp)) })
	}
}

func TestMarkModelPayloadDeliveredRequiresSuccessfulFlush(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyModelPayloadDelivered))
}

func TestStreamServiceContext_InitialValueIsUsable(t *testing.T) {
	ctx := currentStreamServiceContext()

	require.NotNil(t, ctx)
	assert.Nil(t, ctx.Done())
}

func TestSetStreamServiceContext_NormalizesAndStoresContexts(t *testing.T) {
	SetStreamServiceContext(nil)
	t.Cleanup(func() { SetStreamServiceContext(nil) })

	background := context.Background()
	SetStreamServiceContext(background)
	assert.Equal(t, background, currentStreamServiceContext())

	withCancel, cancel := context.WithCancel(context.Background())
	cancel()
	defer cancel()
	SetStreamServiceContext(withCancel)
	assert.Equal(t, withCancel, currentStreamServiceContext())
	select {
	case <-currentStreamServiceContext().Done():
	default:
		t.Fatal("expected context to be canceled")
	}

	SetStreamServiceContext(nil)
	assert.NotNil(t, currentStreamServiceContext())
	assert.Nil(t, currentStreamServiceContext().Done())
}

func TestSetStreamServiceContext_ConsecutiveConcreteTypesDoNotPanic(t *testing.T) {
	contexts := []context.Context{
		context.Background(),
		context.TODO(),
	}
	withCancel, cancel := context.WithCancel(context.Background())
	defer cancel()
	withTimeout, cancelTimeout := context.WithTimeout(context.Background(), time.Minute)
	defer cancelTimeout()
	contexts = append(contexts, withCancel, withTimeout, nil)

	for _, ctx := range contexts {
		assert.NotPanics(t, func() { SetStreamServiceContext(ctx) })
		require.NotNil(t, currentStreamServiceContext())
	}
}

func TestSetStreamServiceContext_ConcurrentLoadStore(t *testing.T) {
	const workers = 8
	const iterations = 1000
	t.Cleanup(func() { SetStreamServiceContext(nil) })

	contexts := []context.Context{context.Background(), context.TODO()}
	withCancel, cancel := context.WithCancel(context.Background())
	defer cancel()
	contexts = append(contexts, withCancel, nil)

	var wg sync.WaitGroup
	var sawNil atomic.Bool
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				SetStreamServiceContext(contexts[(worker+j)%len(contexts)])
			}
		}(i)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if currentStreamServiceContext() == nil {
					sawNil.Store(true)
				}
			}
		}()
	}

	wg.Wait()
	assert.False(t, sawNil.Load())
}
