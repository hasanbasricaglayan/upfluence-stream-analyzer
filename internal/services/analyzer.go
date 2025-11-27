package services

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"upfluence-stream-analyzer/internal/models"
)

// AnalyzerService defines the analyzer service interface
type AnalyzerService interface {
	AnalyzePosts(ctx context.Context, duration time.Duration, dimension string) (*models.AnalysisResult, error)
}

// StreamAnalyzer performs statistical analysis on social media posts
type StreamAnalyzer struct {
	streamClient StreamService
	logger       *slog.Logger
}

// Check interface implementation at compile-time
var _ AnalyzerService = &StreamAnalyzer{}

// NewStreamAnalyzer creates a new stream analyzer
func NewStreamAnalyzer(streamClient StreamService, logger *slog.Logger) *StreamAnalyzer {
	return &StreamAnalyzer{
		streamClient: streamClient,
		logger:       logger,
	}
}

// AnalyzePosts orchestrates the complete analysis workflow.
// Establishes a stream connection with a time-bounded context.
// Collects all posts received within the specified duration.
// Computes statistical analysis on the collected posts.
func (a *StreamAnalyzer) AnalyzePosts(ctx context.Context, duration time.Duration, dimension string) (*models.AnalysisResult, error) {
	// Create context with timeout for the analysis duration
	analyzeCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	// Get stream results
	resultCh, err := a.streamClient.ReadEvents(analyzeCtx)
	if err != nil {
		return nil, err
	}

	// Collect posts from the stream until either:
	// - The context timeout expires (after 'duration')
	// - The stream encounters an error (parse, scanner, network)
	// - The channel closes normally (unexpected, but handled)
	posts, err := a.collectPosts(resultCh)

	// Compute analysis on whatever posts we collected (even if empty or partial)
	result := a.computeAnalysis(posts, dimension)

	// Return result with collection error if one occurred
	if err != nil {
		return result, fmt.Errorf("partial results (collected %d posts): %w", len(posts), err)
	}

	return result, nil
}

// collectPosts continuously reads from the result channel and gathers valid posts.
// Blocks until the channel closes.
func (a *StreamAnalyzer) collectPosts(resultCh <-chan StreamResult) ([]models.PostPayload, error) {
	posts := make([]models.PostPayload, 0)

	for result := range resultCh {
		// Stream error
		if result.Err != nil {
			return posts, result.Err
		}

		// Valid post
		if result.Post != nil {
			posts = append(posts, *result.Post)
			a.logger.Debug("Received post", "type", result.Post.Type, "data", fmt.Sprintf("%v", result.Post.Data))
		}
	}

	// Result channel closed cleanly
	return posts, nil
}

// computeAnalysis computes aggregated metrics from collected posts
func (a *StreamAnalyzer) computeAnalysis(posts []models.PostPayload, dimension string) *models.AnalysisResult {
	// Handle the edge case where no posts were collected during the time window.
	// This can happen if:
	// - The stream had no data
	// - All posts failed validation/parsing
	// - The dimension filter excluded all posts
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

	var dimSum uint64
	var validCount int64

	for _, post := range posts {
		// Update min and max timestamps
		if post.Data.Timestamp < result.MinimumTimestamp {
			result.MinimumTimestamp = post.Data.Timestamp
		}
		if post.Data.Timestamp > result.MaximumTimestamp {
			result.MaximumTimestamp = post.Data.Timestamp
		}

		// Get dimension value
		if dimValue, ok := post.GetDimensionValue(dimension); ok {
			dimSum += dimValue
			validCount++
		}
	}

	// Calculate average with proper rounding
	if validCount > 0 {
		result.Average = int(math.Round(float64(dimSum) / float64(validCount)))
	}

	return result
}
