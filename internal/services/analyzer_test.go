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
func testStreamResultCh(posts []models.PostPayload, err error) <-chan StreamResult {
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

// Helper function to calculate statistics from posts
func testAnalysisResult(posts []models.PostPayload, dimension string) *models.AnalysisResult {
	// Handle the edge case where posts slice is empty
	if len(posts) == 0 {
		return &models.AnalysisResult{
			TotalPosts:       0,
			MinimumTimestamp: 0,
			MaximumTimestamp: 0,
			Average:          0,
		}
	}

	result := &models.AnalysisResult{
		TotalPosts:       len(posts),
		MinimumTimestamp: posts[0].Data.Timestamp,
		MaximumTimestamp: posts[0].Data.Timestamp,
	}

	var dimensionSum uint64
	var validCount int64

	for _, post := range posts {
		// Update min/max timestamps
		if post.Data.Timestamp < result.MinimumTimestamp {
			result.MinimumTimestamp = post.Data.Timestamp
		}
		if post.Data.Timestamp > result.MaximumTimestamp {
			result.MaximumTimestamp = post.Data.Timestamp
		}

		// Get dimension value
		if dimValue, ok := post.GetDimensionValue(dimension); ok {
			dimensionSum += dimValue
			validCount++
		}
	}

	// Calculate average with proper rounding
	if validCount > 0 {
		result.Average = int(math.Round(float64(dimensionSum) / float64(validCount)))
	}

	return result
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

	expectedResult := testAnalysisResult(posts, "likes")

	// Setup mock service that returns some posts then an error
	mockStream := &mockStreamService{
		readEventsFn: func(ctx context.Context) (<-chan StreamResult, error) {
			return testStreamResultCh(posts, streamErr), nil
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
	if result.TotalPosts != expectedResult.TotalPosts {
		t.Errorf("expected TotalPosts=%d, got %d", expectedResult.TotalPosts, result.TotalPosts)
	}
	if result.MinimumTimestamp != expectedResult.MinimumTimestamp {
		t.Errorf("expected MinimumTimestamp=%d, got %d", expectedResult.MinimumTimestamp, result.MinimumTimestamp)
	}
	if result.MaximumTimestamp != expectedResult.MaximumTimestamp {
		t.Errorf("expected MaximumTimestamp=%d, got %d", expectedResult.MinimumTimestamp, result.MaximumTimestamp)
	}
	if result.Average != expectedResult.Average {
		t.Errorf("expected Average=%d, got %d", expectedResult.Average, result.Average)
	}
}

func TestStreamAnalyzer_AnalyzePosts_EmptyStream(t *testing.T) {
	posts := []models.PostPayload{}

	expectedResult := testAnalysisResult(posts, "likes")

	// Setup mock service that returns no posts
	mockStreamClient := &mockStreamService{
		readEventsFn: func(ctx context.Context) (<-chan StreamResult, error) {
			return testStreamResultCh(posts, nil), nil
		},
	}

	analyzer := NewStreamAnalyzer(mockStreamClient, testLogger())

	result, err := analyzer.AnalyzePosts(context.Background(), 1*time.Second, "likes")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should return empty result
	if result.TotalPosts != expectedResult.TotalPosts {
		t.Errorf("expected TotalPosts=%d, got %d", expectedResult.TotalPosts, result.TotalPosts)
	}
	if result.MinimumTimestamp != expectedResult.MinimumTimestamp {
		t.Errorf("expected MinimumTimestamp=%d, got %d", expectedResult.MinimumTimestamp, result.MinimumTimestamp)
	}
	if result.MaximumTimestamp != expectedResult.MaximumTimestamp {
		t.Errorf("expected MaximumTimestamp=%d, got %d", expectedResult.MaximumTimestamp, result.MaximumTimestamp)
	}
	if result.Average != expectedResult.Average {
		t.Errorf("expected Average=%d, got %d", expectedResult.Average, result.Average)
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

	expectedResult := testAnalysisResult(posts, "likes")

	// Setup mock service
	mockStreamClient := &mockStreamService{
		readEventsFn: func(ctx context.Context) (<-chan StreamResult, error) {
			return testStreamResultCh(posts, nil), nil
		},
	}

	analyzer := NewStreamAnalyzer(mockStreamClient, testLogger())

	// Analyze posts with the 'likes' dimension
	result, err := analyzer.AnalyzePosts(context.Background(), 1*time.Second, "likes")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should count posts but average should be 0 (no valid dimension values)
	if result.TotalPosts != expectedResult.TotalPosts {
		t.Errorf("expected TotalPosts=%d, got %d", expectedResult.TotalPosts, result.TotalPosts)
	}
	if result.Average != expectedResult.Average {
		t.Errorf("expected Average=%d when no posts have the dimension, got %d", expectedResult.Average, result.Average)
	}

	// Timestamps should still be tracked
	if result.MinimumTimestamp != expectedResult.MinimumTimestamp {
		t.Errorf("expected MinimumTimestamp=%d, got %d", expectedResult.MinimumTimestamp, result.MinimumTimestamp)
	}
	if result.MaximumTimestamp != expectedResult.MaximumTimestamp {
		t.Errorf("expected MaximumTimestamp=%d, got %d", expectedResult.MaximumTimestamp, result.MaximumTimestamp)
	}
}

func TestStreamAnalyzer_AnalyzePosts_Success(t *testing.T) {
	tests := []struct {
		name      string
		posts     []models.PostPayload
		dimension string
		duration  time.Duration
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
			dimension: "likes",
			duration:  1 * time.Second,
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
			dimension: "likes",
			duration:  1 * time.Second,
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
			dimension: "comments",
			duration:  1 * time.Second,
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
			dimension: "favorites",
			duration:  1 * time.Second,
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
			dimension: "retweets",
			duration:  1 * time.Second,
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
			dimension: "likes",
			duration:  1 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectedResult := testAnalysisResult(tc.posts, tc.dimension)

			// Setup mock stream service
			mockStream := &mockStreamService{
				readEventsFn: func(ctx context.Context) (<-chan StreamResult, error) {
					return testStreamResultCh(tc.posts, nil), nil
				},
			}

			analyzer := NewStreamAnalyzer(mockStream, testLogger())

			// Execute analysis
			result, err := analyzer.AnalyzePosts(context.Background(), tc.duration, tc.dimension)

			// Assertions
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if result.TotalPosts != expectedResult.TotalPosts {
				t.Errorf("expected TotalPosts=%d, got %d", expectedResult.TotalPosts, result.TotalPosts)
			}
			if result.MinimumTimestamp != expectedResult.MinimumTimestamp {
				t.Errorf("expected MinimumTimestamp=%d, got %d", expectedResult.MinimumTimestamp, result.MinimumTimestamp)
			}
			if result.MaximumTimestamp != expectedResult.MaximumTimestamp {
				t.Errorf("expected MaximumTimestamp=%d, got %d", expectedResult.MaximumTimestamp, result.MaximumTimestamp)
			}
			if result.Average != expectedResult.Average {
				t.Errorf("expected Average=%d, got %d", expectedResult.Average, result.Average)
			}
		})
	}
}
