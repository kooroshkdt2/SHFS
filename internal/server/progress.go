package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// broadcastProgress sends progress events to all subscribers.
func (s *Server) broadcastProgress() {
	for evt := range s.progressCh {
		s.progressMu.Lock()
		for ch := range s.progressSubs {
			select {
			case ch <- evt:
			default:
				// drop slow subscribers
			}
		}
		s.progressMu.Unlock()
	}
}

// EmitProgress sends a progress event to all subscribers.
func (s *Server) EmitProgress(evt ProgressEvent) {
	select {
	case s.progressCh <- evt:
	default:
	}
}

// handleProgress is the SSE endpoint for transfer progress.
func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan ProgressEvent, 64)
	s.progressMu.Lock()
	s.progressSubs[ch] = struct{}{}
	s.progressMu.Unlock()

	defer func() {
		s.progressMu.Lock()
		delete(s.progressSubs, ch)
		s.progressMu.Unlock()
		close(ch)
	}()

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
			flusher.Flush()
		}
	}
}

// sendSSE is a helper to send a single SSE event.
func sendSSE(w http.ResponseWriter, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// CountingWriter wraps an io.Writer and counts bytes written.
type CountingWriter struct {
	w       http.ResponseWriter
	written int64
	mu      sync.Mutex
}

func (cw *CountingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.mu.Lock()
	cw.written += int64(n)
	cw.mu.Unlock()
	return n, err
}

func (cw *CountingWriter) Header() http.Header {
	return cw.w.Header()
}

func (cw *CountingWriter) WriteHeader(statusCode int) {
	cw.w.WriteHeader(statusCode)
}

func (cw *CountingWriter) Written() int64 {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return cw.written
}
