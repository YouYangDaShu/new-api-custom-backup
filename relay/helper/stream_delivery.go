package helper

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

type streamDeliveryWriter struct {
	gin.ResponseWriter
	mu       sync.Mutex
	disabled atomic.Bool
	done     chan struct{}
	once     sync.Once
}

func installStreamDeliveryWriter(c *gin.Context) *streamDeliveryWriter {
	if writer, ok := c.Writer.(*streamDeliveryWriter); ok {
		return writer
	}
	writer := &streamDeliveryWriter{ResponseWriter: c.Writer, done: make(chan struct{})}
	c.Writer = writer
	return writer
}
func (w *streamDeliveryWriter) disable() {
	w.mu.Lock()
	w.disabled.Store(true)
	w.mu.Unlock()
	w.once.Do(func() { close(w.done) })
}
func (w *streamDeliveryWriter) deliveryDone() <-chan struct{} { return w.done }
func (w *streamDeliveryWriter) deliveryDisabled() bool        { return w.disabled.Load() }
func (w *streamDeliveryWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.disabled.Load() {
		return len(data), nil
	}
	n, err := w.ResponseWriter.Write(data)
	if err != nil {
		w.disabled.Store(true)
		w.once.Do(func() { close(w.done) })
		return len(data), nil
	}
	return n, nil
}
func (w *streamDeliveryWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }
func (w *streamDeliveryWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.disabled.Load() {
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *streamDeliveryWriter) WriteHeaderNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.disabled.Load() {
		w.ResponseWriter.WriteHeaderNow()
	}
}
func (w *streamDeliveryWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.disabled.Load() {
		w.ResponseWriter.Flush()
	}
}
func (w *streamDeliveryWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.ResponseWriter.Hijack()
}
func (w *streamDeliveryWriter) CloseNotify() <-chan bool { return w.ResponseWriter.CloseNotify() }
func (w *streamDeliveryWriter) Pusher() http.Pusher      { return w.ResponseWriter.Pusher() }
func (w *streamDeliveryWriter) Status() int              { return w.ResponseWriter.Status() }
func (w *streamDeliveryWriter) Size() int                { return w.ResponseWriter.Size() }
func (w *streamDeliveryWriter) Written() bool            { return w.ResponseWriter.Written() }
func streamDeliverySuppressed(c *gin.Context) bool {
	if c == nil {
		return false
	}
	w, ok := c.Writer.(*streamDeliveryWriter)
	return ok && w.deliveryDisabled()
}

type streamServiceContextState struct {
	ctx context.Context
}

var streamServiceContext atomic.Value

func init() {
	streamServiceContext.Store(streamServiceContextState{ctx: context.Background()})
}
func SetStreamServiceContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	streamServiceContext.Store(streamServiceContextState{ctx: ctx})
}
func currentStreamServiceContext() context.Context {
	state, ok := streamServiceContext.Load().(streamServiceContextState)
	if !ok || state.ctx == nil {
		return context.Background()
	}
	return state.ctx
}
