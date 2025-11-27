package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	"upfluence-stream-analyzer/internal/models"
)

// mockStreamService is a mock implementation of the Stream Service for testing
type mockStreamService struct {
	readEventsFn func(ctx context.Context) (<-chan StreamResult, error)
}

func (m *mockStreamService) ReadEvents(ctx context.Context) (<-chan StreamResult, error) {
	if m.readEventsFn != nil {
		return m.readEventsFn(ctx)
	}

	// Return an empty channel by default
	ch := make(chan StreamResult)
	close(ch)
	return ch, nil
}

// testLogger creates a logger that discards output for testing
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Helper function to create a channel with posts and a potential error
func createStreamResultCh(posts []models.PostPayload, err error) <-chan StreamResult {
	ch := make(chan StreamResult, len(posts)+1)

	for _, post := range posts {
		ch <- StreamResult{Post: &post}
	}

	if err != nil {
		ch <- StreamResult{Err: err}
	}

	close(ch)
	return ch
}

func TestStreamAnalyzer_AnalyzePosts_StreamConnectionError(t *testing.T) {
	expectedErr := errors.New("connection refused")

	// Setup mock service that fails to connect to the stream
	mockStreamClient := &mockStreamService{
		readEventsFn: func(ctx context.Context) (<-chan StreamResult, error) {
			return nil, expectedErr
		},
	}

	analyzer := NewStreamAnalyzer(mockStreamClient, testLogger())

	result, err := analyzer.AnalyzePosts(context.Background(), 1*time.Second, "likes")

	// Should return the connection error
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error to wrap %v, got %v", expectedErr, err)
	}

	if result != nil {
		t.Errorf("expected nil result on error, got %v", result)
	}
}

func TestStreamAnalyzer_AnalyzePosts_StreamError(t *testing.T) {
	streamErr := errors.New("parse error")

	posts := []models.PostPayload{
		{
			Type: "tweet",
			Data: models.Post{
				Timestamp: 1554324856,
				Details: map[string]interface{}{
					"likes": 636938,
				},
			},
		},
		{
			Type: "youtube_video",
			Data: models.Post{
				Timestamp: 1633974046,
				Details: map[string]interface{}{
					"likes": 386963,
				},
			},
		},
	}

	expectedAverage := int(math.Round(float64((636938 + 386963)) / 2))

	// Setup mock service that returns some posts then an error
	mockStream := &mockStreamService{
		readEventsFn: func(ctx context.Context) (<-chan StreamResult, error) {
			return createStreamResultCh(posts, streamErr), nil
		},
	}

	analyzer := NewStreamAnalyzer(mockStream, testLogger())

	result, err := analyzer.AnalyzePosts(context.Background(), 1*time.Second, "likes")

	// Should return both partial results and error
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify error contains the original stream error
	if !errors.Is(err, streamErr) {
		t.Errorf("expected error to wrap %v, got %v", streamErr, err)
	}

	// Verify error message mentions partial results
	if !strings.Contains(err.Error(), "partial results") {
		t.Errorf("expected error message to mention 'partial results', got: %v", err.Error())
	}

	// Result should not be nil
	if result == nil {
		t.Fatal("expected non-nil result with partial data, got nil")
	}

	// Verify partial results are computed correctly
	if result.TotalPosts != 2 {
		t.Errorf("expected TotalPosts=2, got %d", result.TotalPosts)
	}
	if result.MinimumTimestamp != 1554324856 {
		t.Errorf("expected MinimumTimestamp=1554324856, got %d", result.MinimumTimestamp)
	}
	if result.MaximumTimestamp != 1633974046 {
		t.Errorf("expected MaximumTimestamp=1633974046, got %d", result.MaximumTimestamp)
	}
	if result.Average != expectedAverage {
		t.Errorf("expected Average=%d, got %d", expectedAverage, result.Average)
	}
}

func TestStreamAnalyzer_AnalyzePosts_EmptyStream(t *testing.T) {
	// Setup mock service that returns no posts
	mockStreamClient := &mockStreamService{
		readEventsFn: func(ctx context.Context) (<-chan StreamResult, error) {
			return createStreamResultCh([]models.PostPayload{}, nil), nil
		},
	}

	analyzer := NewStreamAnalyzer(mockStreamClient, testLogger())

	result, err := analyzer.AnalyzePosts(context.Background(), 1*time.Second, "likes")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should return empty result
	if result.TotalPosts != 0 {
		t.Errorf("expected TotalPosts=0, got %d", result.TotalPosts)
	}
	if result.MinimumTimestamp != 0 {
		t.Errorf("expected MinimumTimestamp=0, got %d", result.MinimumTimestamp)
	}
	if result.MaximumTimestamp != 0 {
		t.Errorf("expected MaximumTimestamp=0, got %d", result.MaximumTimestamp)
	}
	if result.Average != 0 {
		t.Errorf("expected Average=0, got %d", result.Average)
	}
}

func TestStreamAnalyzer_AnalyzePosts_AllPostsMissingDimension(t *testing.T) {
	posts := []models.PostPayload{
		{
			Type: "article",
			Data: models.Post{
				Timestamp: 1554324856,
				// No 'likes' field
				Details: map[string]interface{}{},
			},
		},
		{
			Type: "facebook_status",
			Data: models.Post{
				Timestamp: 1633974046,
				// No 'likes' field
				Details: map[string]interface{}{},
			},
		},
	}

	// Setup mock service
	mockStreamClient := &mockStreamService{
		readEventsFn: func(ctx context.Context) (<-chan StreamResult, error) {
			return createStreamResultCh(posts, nil), nil
		},
	}

	analyzer := NewStreamAnalyzer(mockStreamClient, testLogger())

	// Analyze posts with the 'likes' dimension
	result, err := analyzer.AnalyzePosts(context.Background(), 1*time.Second, "likes")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should count posts but average should be 0 (no valid dimension values)
	if result.TotalPosts != 2 {
		t.Errorf("expected TotalPosts=2, got %d", result.TotalPosts)
	}
	if result.Average != 0 {
		t.Errorf("expected Average=0 when no posts have the dimension, got %d", result.Average)
	}

	// Timestamps should still be tracked
	if result.MinimumTimestamp != 1554324856 {
		t.Errorf("expected MinimumTimestamp=1554324856, got %d", result.MinimumTimestamp)
	}
	if result.MaximumTimestamp != 1633974046 {
		t.Errorf("expected MaximumTimestamp=1633974046, got %d", result.MaximumTimestamp)
	}
}

func TestStreamAnalyzer_ComputeAnalysis_EmptyPosts(t *testing.T) {
	mockStreamClient := &mockStreamService{}

	analyzer := NewStreamAnalyzer(mockStreamClient, testLogger())

	// Call computeAnalysis directly with an empty slice
	result := analyzer.computeAnalysis([]models.PostPayload{}, "likes")

	// Should handle empty slice
	if result.TotalPosts != 0 {
		t.Errorf("expected TotalPosts=0, got %d", result.TotalPosts)
	}
	if result.MinimumTimestamp != 0 {
		t.Errorf("expected MinimumTimestamp=0, got %d", result.MinimumTimestamp)
	}
	if result.MaximumTimestamp != 0 {
		t.Errorf("expected MaximumTimestamp=0, got %d", result.MaximumTimestamp)
	}
	if result.Average != 0 {
		t.Errorf("expected Average=0, got %d", result.Average)
	}
}

func TestStreamAnalyzer_ComputeAnalysis_TimestampOrdering(t *testing.T) {
	// Posts not in chronological order
	posts := []models.PostPayload{
		{
			Type: "tweet",
			Data: models.Post{
				Timestamp: 1633974046,
				Details: map[string]interface{}{
					"likes": 636938,
				},
			},
		},
		{
			Type: "youtube_video",
			Data: models.Post{
				// Earliest timestamp
				Timestamp: 1554324856,
				Details: map[string]interface{}{
					"likes": 386963,
				},
			},
		},
		{
			Type: "pin",
			Data: models.Post{
				// Latest timestamp
				Timestamp: 1764164027,
				Details: map[string]interface{}{
					"likes": 636938,
				},
			},
		},
		{
			Type: "instagram_media",
			Data: models.Post{
				Timestamp: 1763217054,
				Details: map[string]interface{}{
					"likes": 386963,
				},
			},
		},
	}

	mockStreamClient := &mockStreamService{}

	analyzer := NewStreamAnalyzer(mockStreamClient, testLogger())

	result := analyzer.computeAnalysis(posts, "likes")

	// Should find correct min and max timestamps regardless of order
	if result.MinimumTimestamp != 1554324856 {
		t.Errorf("expected MinimumTimestamp=1554324856, got %d", result.MinimumTimestamp)
	}
	if result.MaximumTimestamp != 1764164027 {
		t.Errorf("expected MaximumTimestamp=1764164027, got %d", result.MaximumTimestamp)
	}
}

func TestStreamAnalyzer_ComputeAnalysis_LargeNumbers(t *testing.T) {
	posts := []models.PostPayload{
		{
			Type: "youtube_video",
			Data: models.Post{
				Timestamp: 1764164027,
				Details: map[string]interface{}{
					"likes": 55475432,
				},
			},
		},
		{
			Type: "instagram_media",
			Data: models.Post{
				Timestamp: 1763217054,
				Details: map[string]interface{}{
					"likes": 74673010,
				},
			},
		},
	}

	expectedAverage := int(math.Round(float64((55475432 + 74673010)) / 2))

	mockStreamClient := &mockStreamService{}

	analyzer := NewStreamAnalyzer(mockStreamClient, testLogger())

	result := analyzer.computeAnalysis(posts, "likes")

	// Should handle large numbers correctly
	if result.Average != expectedAverage {
		t.Errorf("expected Average=%d, got %d", expectedAverage, result.Average)
	}
}
