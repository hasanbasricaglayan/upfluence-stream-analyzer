package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var logger = testLogger()

func TestStreamClient_NewStreamClient(t *testing.T) {
	url := "https://example.com/stream"

	client := NewStreamClient(url, logger)

	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.url != url {
		t.Errorf("expected url %s, got %s", url, client.url)
	}
	if client.logger != logger {
		t.Error("expected logger to be set")
	}

	if client.httpClient == nil {
		t.Fatal("expected non-nil http client")
	}
	if client.httpClient.Timeout != 0 {
		t.Errorf("expected zero timeout for streaming, got %v", client.httpClient.Timeout)
	}
}

func TestStreamClient_ReadEvents_ConnectionError(t *testing.T) {
	// Use invalid URL to force connection error
	url := "https://invalid.com/stream"

	client := NewStreamClient(url, logger)

	ctx := context.Background()

	resultCh, err := client.ReadEvents(ctx)

	if err == nil {
		t.Fatal("expected connection error, got nil")
	}

	if resultCh != nil {
		t.Errorf("expected nil channel on connection error, got %v", resultCh)
	}

	if !strings.Contains(err.Error(), "failed to connect to stream") {
		t.Errorf("expected error message to contain 'failed to connect to stream', got: %v", err)
	}
}

func TestStreamClient_ReadEvents_NonOKStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"bad request", http.StatusBadRequest},
		{"unauthorized", http.StatusUnauthorized},
		{"forbidden", http.StatusForbidden},
		{"not found", http.StatusNotFound},
		{"internal server error", http.StatusInternalServerError},
		{"service unavailable", http.StatusServiceUnavailable},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			client := NewStreamClient(server.URL, logger)

			ctx := context.Background()

			resultCh, err := client.ReadEvents(ctx)

			if err == nil {
				t.Fatal("expected error for non-200 status, got nil")
			}

			if resultCh != nil {
				t.Errorf("expected nil channel on error, got %v", resultCh)
			}

			expectedErrMessage := "unexpected status code"
			if !strings.Contains(err.Error(), expectedErrMessage) {
				t.Errorf("expected error to contain %q, got: %v", expectedErrMessage, err)
			}
		})
	}
}

func TestStreamClient_ReadEvents_ContextDeadlineExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send posts indefinitely
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				w.Write([]byte(`data: {"tweet":{"timestamp":1633974046,"likes":386963}}` + "\n"))
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			case <-r.Context().Done():
				return
			}
		}
	}))
	defer server.Close()

	client := NewStreamClient(server.URL, logger)

	// Very short deadline (i.e. very short 'duration')
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	resultCh, err := client.ReadEvents(ctx)

	if err != nil {
		t.Fatalf("expected no error on connection, got %v", err)
	}

	// Consume until channel closes
	for range resultCh {
		// Just drain the channel
	}

	// Verify context deadline was exceeded
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Errorf("expected context deadline exceeded, got: %v", ctx.Err())
	}
}

func TestStreamClient_ReadEvents_ContextCancellation(t *testing.T) {
	// Server that keeps connection open
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send one event then block
		w.Write([]byte(`data: {"tweet":{"timestamp":1554324856,"likes":636938}}` + "\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Block until context is cancelled
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewStreamClient(server.URL, logger)

	ctx, cancel := context.WithCancel(context.Background())

	resultCh, err := client.ReadEvents(ctx)

	if err != nil {
		t.Fatalf("expected no error on connection, got %v", err)
	}

	// Read first post
	result := <-resultCh
	if result.Post == nil {
		t.Fatal("expected first post")
	}

	// Cancel context
	cancel()

	// Channel should close
	timeout := time.After(2 * time.Second)
	channelClosed := false
	for !channelClosed {
		select {
		case _, ok := <-resultCh:
			if !ok {
				channelClosed = true
			}
		case <-timeout:
			t.Fatal("channel did not close after context cancellation")
		}
	}

	// Verify context cancellation
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("expected context canceled, got: %v", ctx.Err())
	}
}

func TestStreamClient_ReadEvents_ParseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send valid event
		w.Write([]byte(`data: {"tweet":{"timestamp":1554324856,"likes":636938}}` + "\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Send invalid JSON
		w.Write([]byte(`data: {invalid json}` + "\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	client := NewStreamClient(server.URL, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	resultCh, err := client.ReadEvents(ctx)

	if err != nil {
		t.Fatalf("expected no error on connection, got %v", err)
	}

	// First result should be valid post
	result1 := <-resultCh
	if result1.Post == nil {
		t.Error("expected first result to have a post")
	}
	if result1.Err != nil {
		t.Errorf("expected first result to have no error, got %v", result1.Err)
	}

	// Second result should be error
	result2 := <-resultCh
	if result2.Err == nil {
		t.Error("expected second result to have error for invalid JSON")
	}
	if result2.Post != nil {
		t.Error("expected second result to have no post")
	}

	// Verify error message
	if !strings.Contains(result2.Err.Error(), "parse error") && !strings.Contains(result2.Err.Error(), "stream error") {
		t.Errorf("expected error to mention parse/stream error, got: %v", result2.Err)
	}

	// Channel should close after error
	_, ok := <-resultCh
	if ok {
		t.Error("expected channel to be closed after parse error")
	}
}

func TestStreamClient_ReadEvents_EmptyLines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Mix of empty lines and valid events
		events := []string{
			"", // Empty line
			`data: {"tweet":{"timestamp":1554324856,"likes":636938}}`,
			"", // Empty line
			"", // Another empty line
			`data: {"instagram_media":{"timestamp":1633974046,"comments":386963}}`,
			"", // Empty line
		}

		for _, event := range events {
			w.Write([]byte(event + "\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	client := NewStreamClient(server.URL, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	resultCh, err := client.ReadEvents(ctx)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should receive exactly 2 posts (empty lines skipped)
	posts := 0
	for result := range resultCh {
		if result.Err != nil {
			t.Errorf("unexpected error: %v", result.Err)
			continue
		}
		if result.Post != nil {
			posts++
		}
	}
	if posts != 2 {
		t.Errorf("expected 2 posts (empty lines should be skipped), got %d", posts)
	}
}

func TestStreamClient_ReadEvents_NonDataLines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Mix of data lines and non-data lines
		lines := []string{
			`event: message`, // Event field
			`data: {"tweet":{"timestamp":1554324856,"likes":636938}}`, // Data field
			`id: 123`, // ID field
			`data: {"instagram_media":{"timestamp":1633974046,"comments":386963}}`, // Data field
		}

		for _, line := range lines {
			w.Write([]byte(line + "\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	client := NewStreamClient(server.URL, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	resultCh, err := client.ReadEvents(ctx)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should only receive data lines
	posts := 0
	for result := range resultCh {
		if result.Err != nil {
			t.Errorf("unexpected error: %v", result.Err)
			continue
		}
		if result.Post != nil {
			posts++
		}
	}
	if posts != 2 {
		t.Errorf("expected 2 posts (non-data lines should be ignored), got %d", posts)
	}
}

func TestStreamClient_ReadEvents_ChannelBuffer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send more events than buffer size to verify no blocking
		for range 200 {
			event := `data: {"tweet":{"timestamp":1633974046,"likes":386963}}` + "\n"
			w.Write([]byte(event))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer server.Close()

	client := NewStreamClient(server.URL, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resultCh, err := client.ReadEvents(ctx)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Slowly consume to test buffering
	posts := 0
	for result := range resultCh {
		if result.Err != nil {
			continue
		}
		if result.Post != nil {
			posts++
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Should receive at least 100 posts despite slow consumption
	if posts < 100 {
		t.Errorf("expected at least 100 posts with buffering, got %d", posts)
	}
}
