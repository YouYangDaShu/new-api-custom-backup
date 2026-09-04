package common

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type StreamEndReason string

const (
	StreamEndReasonNone        StreamEndReason = ""
	StreamEndReasonDone        StreamEndReason = "done"
	StreamEndReasonTimeout     StreamEndReason = "timeout"
	StreamEndReasonClientGone  StreamEndReason = "client_gone"
	StreamEndReasonScannerErr  StreamEndReason = "scanner_error"
	StreamEndReasonHandlerStop StreamEndReason = "handler_stop"
	StreamEndReasonEOF         StreamEndReason = "eof"
	StreamEndReasonPanic       StreamEndReason = "panic"
	StreamEndReasonPingFail    StreamEndReason = "ping_fail"
)

const maxStreamErrorEntries = 20

type StreamErrorEntry struct {
	Message   string
	Timestamp time.Time
}

type StreamStatus struct {
	// Deprecated compatibility fields; use Snapshot/ErrorsSnapshot in concurrent code.
	EndReason  StreamEndReason
	EndError   error
	Errors     []StreamErrorEntry
	ErrorCount int
	endReason  StreamEndReason
	endError   error
	mu         sync.Mutex
	errors     []StreamErrorEntry
	errorCount int
}

func NewStreamStatus() *StreamStatus {
	return &StreamStatus{}
}

func (s *StreamStatus) SetEndReason(reason StreamEndReason, err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.endReason == StreamEndReasonNone || (reason == StreamEndReasonHandlerStop && (s.endReason == StreamEndReasonDone || s.endReason == StreamEndReasonEOF)) {
		s.endReason, s.endError = reason, err
		s.EndReason, s.EndError = reason, err
	}
}

func (s *StreamStatus) ReplaceEndReason(reason StreamEndReason, err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.endReason == StreamEndReasonClientGone && reason == StreamEndReasonTimeout {
		return
	}
	s.endReason, s.endError = reason, err
	s.EndReason, s.EndError = reason, err
}

func (s *StreamStatus) Snapshot() (StreamEndReason, error) {
	if s == nil {
		return StreamEndReasonNone, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endReason, s.endError
}

func (s *StreamStatus) RecordError(msg string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errorCount++
	s.ErrorCount = s.errorCount
	if len(s.errors) < maxStreamErrorEntries {
		s.errors = append(s.errors, StreamErrorEntry{
			Message:   msg,
			Timestamp: time.Now(),
		})
		s.Errors = append([]StreamErrorEntry(nil), s.errors...)
	}
}

func (s *StreamStatus) HasErrors() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.errorCount > 0
}

func (s *StreamStatus) TotalErrorCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.errorCount
}

func (s *StreamStatus) ErrorsSnapshot() (int, []StreamErrorEntry) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	errs := append([]StreamErrorEntry(nil), s.errors...)
	return s.errorCount, errs
}

func (s *StreamStatus) IsNormalEnd() bool {
	if s == nil {
		return true
	}
	reason, err := s.Snapshot()
	return (reason == StreamEndReasonDone || reason == StreamEndReasonEOF || reason == StreamEndReasonHandlerStop) && err == nil
}

func (s *StreamStatus) Summary() string {
	if s == nil {
		return "StreamStatus<nil>"
	}
	b := &strings.Builder{}
	reason, endErr := s.Snapshot()
	fmt.Fprintf(b, "reason=%s", reason)
	if endErr != nil {
		fmt.Fprintf(b, " end_error=%q", endErr.Error())
	}
	s.mu.Lock()
	if s.errorCount > 0 {
		fmt.Fprintf(b, " soft_errors=%d", s.errorCount)
	}
	s.mu.Unlock()
	return b.String()
}
