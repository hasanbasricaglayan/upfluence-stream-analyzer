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

// Check interface implementation at compile-time
var _ StreamService = &mockStreamService{}

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

func TestStreamAnalyzer_AnalyzePosts_Success(t *testing.T) {
	tests := []struct {
		name                 string
		posts                []models.PostPayload
		dimension            string
		duration             time.Duration
		expectedTotal        int
		expectedMinTimestamp int64
		expectedMaxTimestamp int64
		expectedAvg          int
	}{
		{
			name: "single post with likes",
			posts: []models.PostPayload{
				{
					Type: "tweet",
					Data: models.Post{
						Timestamp: 1554324856,
						Details: map[string]interface{}{
							"likes": 636938,
						},
					},
				},
			},
			dimension:            "likes",
			duration:             1 * time.Second,
			expectedTotal:        1,
			expectedMinTimestamp: 1554324856,
			expectedMaxTimestamp: 1554324856,
			expectedAvg:          636938,
		},
		{
			name: "multiple posts with varying likes",
			posts: []models.PostPayload{
				{
					Type: "tweet",
					Data: models.Post{
						Timestamp: 1554324856,
						Details: map[string]interface{}{
							"likes": 50,
						},
					},
				},
				{
					Type: "youtube_video",
					Data: models.Post{
						Timestamp: 1633974046,
						Details: map[string]interface{}{
							"likes": 150,
						},
					},
				},
				{
					Type: "instagram_media",
					Data: models.Post{
						Timestamp: 1738974078,
						Details: map[string]interface{}{
							"likes": 100,
						},
					},
				},
			},
			dimension:            "likes",
			duration:             1 * time.Second,
			expectedTotal:        3,
			expectedMinTimestamp: 1554324856,
			expectedMaxTimestamp: 1738974078,
			expectedAvg:          100, // (50 + 150 + 100) / 3 = 100
		},
		{
			name: "posts with comments dimension",
			posts: []models.PostPayload{
				{
					Type: "tweet",
					Data: models.Post{
						Timestamp: 1554324856,
						Details: map[string]interface{}{
							"comments": 10,
						},
					},
				},
				{
					Type: "youtube_video",
					Data: models.Post{
						Timestamp: 1633974046,
						Details: map[string]interface{}{
							"comments": 20,
						},
					},
				},
				{
					Type: "instagram_media",
					Data: models.Post{
						Timestamp: 1738974078,
						Details: map[string]interface{}{
							"comments": 0,
						},
					},
				},
			},
			dimension:            "comments",
			duration:             1 * time.Second,
			expectedTotal:        3,
			expectedMinTimestamp: 1554324856,
			expectedMaxTimestamp: 1738974078,
			expectedAvg:          10, // (10 + 20 + 0) / 3 = 10
		},
		{
			name: "posts with favorites dimension",
			posts: []models.PostPayload{
				{
					Type: "tweet",
					Data: models.Post{
						Timestamp: 1554324856,
						Details: map[string]interface{}{
							"favorites": 5,
						},
					},
				},
				{
					Type: "youtube_video",
					Data: models.Post{
						Timestamp: 1633974046,
						Details: map[string]interface{}{
							"favorites": 15,
						},
					},
				},
			},
			dimension:            "favorites",
			duration:             1 * time.Second,
			expectedTotal:        2,
			expectedMinTimestamp: 1554324856,
			expectedMaxTimestamp: 1633974046,
			expectedAvg:          10, // (5 + 15) / 2 = 10
		},
		{
			name: "posts with retweets dimension",
			posts: []models.PostPayload{
				{
					Type: "tweet",
					Data: models.Post{
						Timestamp: 1633974046,
						Details: map[string]interface{}{
							"retweets": 25,
						},
					},
				},
				{
					Type: "tweet",
					Data: models.Post{
						Timestamp: 1738974078,
						Details: map[string]interface{}{
							"retweets": 75,
						},
					},
				},
			},
			dimension:            "retweets",
			duration:             1 * time.Second,
			expectedTotal:        2,
			expectedMinTimestamp: 1633974046,
			expectedMaxTimestamp: 1738974078,
			expectedAvg:          50, // (25 + 75) / 2 = 50
		},
		{
			name: "posts with some missing dimension values",
			posts: []models.PostPayload{
				{
					Type: "tweet",
					Data: models.Post{
						Timestamp: 1633974046,
						Details: map[string]interface{}{
							"likes": 100,
						},
					},
				},
				{
					Type: "article",
					Data: models.Post{
						Timestamp: 1738974078,
						// No 'likes' field
						Details: map[string]interface{}{},
					},
				},
				{
					Type: "tweet",
					Data: models.Post{
						Timestamp: 1554324856,
						Details: map[string]interface{}{
							"likes": 200,
						},
					},
				},
			},
			dimension:            "likes",
			duration:             1 * time.Second,
			expectedTotal:        3, // All posts counted
			expectedMinTimestamp: 1554324856,
			expectedMaxTimestamp: 1738974078,
			expectedAvg:          150, // (100 + 200) / 2 = 150 (article excluded from average)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock stream service
			mockStream := &mockStreamService{
				readEventsFn: func(ctx context.Context) (<-chan StreamResult, error) {
					return createStreamResultCh(tt.posts, nil), nil
				},
			}

			analyzer := NewStreamAnalyzer(mockStream, testLogger())

			// Execute analysis
			result, err := analyzer.AnalyzePosts(context.Background(), tt.duration, tt.dimension)

			// Assertions
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if result.TotalPosts != tt.expectedTotal {
				t.Errorf("expected TotalPosts=%d, got %d", tt.expectedTotal, result.TotalPosts)
			}
			if result.MinimumTimestamp != tt.expectedMinTimestamp {
				t.Errorf("expected MinimumTimestamp=%d, got %d", tt.expectedMinTimestamp, result.MinimumTimestamp)
			}
			if result.MaximumTimestamp != tt.expectedMaxTimestamp {
				t.Errorf("expected MaximumTimestamp=%d, got %d", tt.expectedMaxTimestamp, result.MaximumTimestamp)
			}
			if result.Average != tt.expectedAvg {
				t.Errorf("expected Average=%d, got %d", tt.expectedAvg, result.Average)
			}
		})
	}
}
