package services

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"upfluence-stream-analyzer/internal/models"
)

// StreamResult wraps either a post or an error from the stream
type StreamResult struct {
	Post *models.PostPayload
	Err  error
}

// StreamService defines the stream service interface
type StreamService interface {
	ReadEvents(ctx context.Context) (<-chan StreamResult, error)
}

// StreamClient manages stream connection and reads events
type StreamClient struct {
	url        string
	logger     *slog.Logger
	httpClient *http.Client
}

// Check interface implementation at compile-time
var _ StreamService = &StreamClient{}

// NewStreamClient creates a new stream client
func NewStreamClient(url string, logger *slog.Logger) *StreamClient {
	return &StreamClient{
		url:    url,
		logger: logger,

		// No timeout for streaming connection
		httpClient: &http.Client{Timeout: 0},
	}
}

// ReadEvents connects to the stream and sends post events to the result channel.
// Returns an error if initial connection to the stream fails.
// The channel is closed when the context is cancelled or stream ends unexpectedly.
func (c *StreamClient) ReadEvents(ctx context.Context) (<-chan StreamResult, error) {
	// Establish connection to the stream
	resp, err := c.getStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	c.logger.Info("Stream connection established")

	// Connection successful, start reading events asynchronously
	resultCh := make(chan StreamResult, 100)

	go c.readStream(ctx, resp.Body, resultCh)

	return resultCh, nil
}

// getStream establishes HTTP connection to Upfluence's SSE stream endpoint
func (c *StreamClient) getStream(ctx context.Context) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request to get events from the stream: %w", err)
	}

	// These headers are needed in case of an SSE
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to get events from the stream: %w", err)
	}

	return resp, nil
}

// readStream manages the lifecycle of a single SSE connection.
// Reads events from the stream, parses and sends them to the result channel.
// The channel is closed when the function exits.
func (c *StreamClient) readStream(ctx context.Context, body io.ReadCloser, resultCh chan<- StreamResult) {
	defer close(resultCh)
	defer body.Close()

	// Consume events (posts) coming from the stream by parsing and pushing them to the result channel
	err := c.consumeStream(ctx, body, resultCh)

	switch {
	case err == nil:
		// Stream ended normally with an EOF (this is not supposed to happen)
		c.logger.Info("Stream ended normally")

	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		// Context cancellation is normal and expected (due to duration parameter)
		c.logger.Info("Stream connection stopped", "reason", err.Error())

	default:
		// Anything else is an unexpected error (parse, scanner, network) and is sent to the collector
		c.logger.Error("Stream error", "err", err.Error())
		resultCh <- StreamResult{Err: fmt.Errorf("stream error: %w", err)}
	}
}

// consumeStream reads and processes SSE events line by line.
// Returns nil on normal EOF, context error on cancellation, or other errors (parse, scanner, network).
func (c *StreamClient) consumeStream(ctx context.Context, r io.Reader, resultCh chan<- StreamResult) error {
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		// Check the context once per iteration
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		b := scanner.Bytes()

		// Skip empty lines
		if len(b) == 0 {
			continue
		}

		// SSE data lines start with "data: " prefix
		if event, ok := bytes.CutPrefix(b, []byte("data: ")); ok {
			// handleEvent respects the context
			if err := c.handleEvent(ctx, event, resultCh); err != nil {
				return err
			}
		}
	}

	// Check if the scanner stopped due to context cancellation
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	// Stream ended normally with an EOF (this is not supposed to happen)
	return nil
}

// handleEvent parses a single SSE event and sends it to the result channel.
// Returns a non-nil error if parsing fails or the context is cancelled.
// Blocks until the event is sent or the context is cancelled.
func (c *StreamClient) handleEvent(ctx context.Context, event []byte, resultCh chan<- StreamResult) error {
	var post models.PostPayload

	if err := post.UnmarshalJSON(event); err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// Send post to the channel, respecting context cancellation
	select {
	case resultCh <- StreamResult{Post: &post}:
		// Successfully sent
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
