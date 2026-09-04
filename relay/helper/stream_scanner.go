package helper

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

const (
	InitialScannerBufferSize    = 64 << 10
	DefaultMaxScannerBufferSize = 128 << 20
	DefaultPingInterval         = 10 * time.Second
	streamWriteTimeout          = 30 * time.Second
)

var (
	streamDrainTimeout         = time.Second
	streamDrainMaxEvents int64 = 2048
	streamDrainSem             = make(chan struct{}, 32)
)

func getScannerBufferSize() int {
	if constant.StreamScannerMaxBufferMB > 0 {
		return constant.StreamScannerMaxBufferMB << 20
	}
	return DefaultMaxScannerBufferSize
}
func NewStreamScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, InitialScannerBufferSize), getScannerBufferSize())
	return scanner
}
func ExtendWriteDeadline(c *gin.Context) {
	if c == nil || c.Writer == nil || streamDeliverySuppressed(c) {
		return
	}
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(streamWriteTimeout))
}

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *StreamResult)) {
	if c == nil || c.Request == nil || resp == nil || resp.Body == nil || info == nil || dataHandler == nil {
		return
	}
	info.StreamStatus = relaycommon.NewStreamStatus()
	delivery := installStreamDeliveryWriter(c)
	SetEventStreamHeaders(c)
	workCtx, cancelWork := context.WithCancel(context.Background())
	defer cancelWork()
	var closeOnce sync.Once
	closeBody := func() { closeOnce.Do(func() { _ = resp.Body.Close() }) }
	defer closeBody()
	streamingTimeout := time.Duration(constant.StreamingTimeout) * time.Second
	if streamingTimeout <= 0 {
		streamingTimeout = 30 * time.Second
	}
	activity := make(chan struct{}, 1)
	dataChan := make(chan string, 10)
	scannerDone := make(chan struct{})
	handlerDone := make(chan struct{})
	parserStop := make(chan struct{})
	var parserStopOnce sync.Once
	var draining atomic.Bool
	var drainEvents atomic.Int64
	requestCtx := c.Request.Context()
	go func() {
		select {
		case <-requestCtx.Done():
			draining.Store(true)
			delivery.disable()
		case <-workCtx.Done():
		}
	}()
	go func() {
		defer close(handlerDone)
		defer func() {
			if r := recover(); r != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("handler panic: %v", r))
			}
		}()
		sr := newStreamResult(info.StreamStatus)
		for data := range dataChan {
			sr.reset()
			ExtendWriteDeadline(c)
			dataHandler(data, sr)
			if sr.IsStopped() {
				parserStopOnce.Do(func() { close(parserStop) })
				return
			}
		}
	}()
	go func() {
		defer close(scannerDone)
		defer close(dataChan)
		defer func() {
			if r := recover(); r != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("scanner panic: %v", r))
			}
		}()
		scanner := NewStreamScanner(resp.Body)
		scanner.Split(bufio.ScanLines)
		for scanner.Scan() {
			select {
			case activity <- struct{}{}:
			default:
			}
			line := scanner.Text()
			logger.LogDebug(c, "stream scanner data: %s", line)
			if len(line) < 6 || (line[:5] != "data:" && line[:6] != "[DONE]") {
				continue
			}
			data := strings.TrimSpace(line[5:])
			if data == "" {
				continue
			}
			if strings.HasPrefix(data, "[DONE]") {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				return
			}
			if draining.Load() && drainEvents.Add(1) > streamDrainMaxEvents {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, fmt.Errorf("stream drain event limit reached"))
				return
			}
			info.SetFirstResponseTime()
			info.ReceivedResponseCount++
			select {
			case dataChan <- data:
			case <-workCtx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil && err != io.EOF && workCtx.Err() == nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
			return
		}
		if workCtx.Err() == nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
		}
	}()

	settings := operation_setting.GetGeneralSetting()
	pingEnabled := settings.PingIntervalEnabled && !info.DisablePing
	pingInterval := time.Duration(settings.PingIntervalSeconds) * time.Second
	if pingInterval <= 0 {
		pingInterval = DefaultPingInterval
	}
	var pingTicker *time.Ticker
	if pingEnabled {
		pingTicker = time.NewTicker(pingInterval)
		defer pingTicker.Stop()
	}
	idleTimer := time.NewTimer(streamingTimeout)
	defer idleTimer.Stop()
	var drainTimer *time.Timer
	var drainTimerC <-chan time.Time
	drainSlot := false
	defer func() {
		if drainSlot {
			<-streamDrainSem
		}
	}()
	resetIdle := func() {
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(streamingTimeout)
	}
	finished := false
	for !finished {
		select {
		case <-activity:
			resetIdle()
		case <-delivery.deliveryDone():
			if drainTimerC != nil {
				continue
			}
			draining.Store(true)
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, requestCtx.Err())
			select {
			case streamDrainSem <- struct{}{}:
				drainSlot = true
				drainTimer = time.NewTimer(streamDrainTimeout)
				drainTimerC = drainTimer.C
			default:
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, fmt.Errorf("stream drain concurrency limit reached"))
				cancelWork()
				closeBody()
			}
		case <-parserStop:
			cancelWork()
			closeBody()
		case <-scannerDone:
			<-handlerDone
			finished = true
		case <-idleTimer.C:
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, fmt.Errorf("stream idle timeout"))
			cancelWork()
			closeBody()
		case <-drainTimerC:
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, fmt.Errorf("stream drain timeout"))
			cancelWork()
			closeBody()
			drainTimerC = nil
		case <-currentStreamServiceContext().Done():
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, fmt.Errorf("stream drain canceled by service shutdown"))
			delivery.disable()
			cancelWork()
			closeBody()
		case <-func() <-chan time.Time {
			if pingTicker == nil {
				return nil
			}
			return pingTicker.C
		}():
			ExtendWriteDeadline(c)
			if PingData(c) != nil {
				delivery.disable()
			}
		}
	}
	if drainTimer != nil {
		drainTimer.Stop()
	}
	if draining.Load() && info.StreamStatus.EndReason != relaycommon.StreamEndReasonTimeout && info.StreamStatus.EndReason != relaycommon.StreamEndReasonPanic && info.StreamStatus.EndReason != relaycommon.StreamEndReasonScannerErr {
		info.StreamStatus.ReplaceEndReason(relaycommon.StreamEndReasonClientGone, requestCtx.Err())
	}
	if info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors() {
		logger.LogInfo(c, fmt.Sprintf("stream ended: %s", info.StreamStatus.Summary()))
	} else {
		logger.LogError(c, fmt.Sprintf("stream ended: %s, received=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount))
	}
}
