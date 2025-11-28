package services

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/hasanbasricaglayan/upfluence-stream-analyzer/internal/models"
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

// aggregator computes statistics incrementally without storing posts
type aggregator struct {
	totalPosts       int
	minimumTimestamp int64
	maximumTimestamp int64
	dimensionSum     uint64
	validCount       int64
	dimension        string
}

// newAggregator creates a new aggregator
func newAggregator(dimension string) *aggregator {
	return &aggregator{
		totalPosts:       0,
		minimumTimestamp: 0,
		maximumTimestamp: 0,
		dimensionSum:     0,
		validCount:       0,
		dimension:        dimension,
	}
}

// processPost updates the aggregator with a new post (incremental computation)
func (agg *aggregator) processPost(post *models.PostPayload) {
	// Increment total count
	agg.totalPosts++

	timestamp := post.Data.Timestamp

	// Update min/max timestamps
	if agg.totalPosts == 1 {
		// First post
		agg.minimumTimestamp = timestamp
		agg.maximumTimestamp = timestamp
	} else {
		// Subsequent posts
		if timestamp < agg.minimumTimestamp {
			agg.minimumTimestamp = timestamp
		}
		if timestamp > agg.maximumTimestamp {
			agg.maximumTimestamp = timestamp
		}
	}

	// Update dimension statistics
	if dimValue, ok := post.GetDimensionValue(agg.dimension); ok {
		agg.dimensionSum += dimValue
		agg.validCount++
	}
}

// getResult computes the final result from accumulated statistics
func (agg *aggregator) getResult() *models.AnalysisResult {
	result := &models.AnalysisResult{
		TotalPosts:       agg.totalPosts,
		MinimumTimestamp: agg.minimumTimestamp,
		MaximumTimestamp: agg.maximumTimestamp,
		Average:          0,
	}

	// Calculate average with proper rounding
	if agg.validCount > 0 {
		result.Average = int(math.Round(float64(agg.dimensionSum) / float64(agg.validCount)))
	}

	return result
}

// AnalyzePosts orchestrates the complete analysis workflow.
// Establishes a stream connection with a time-bounded context.
// Posts are analyzed as they arrive using incremental computation (no memory storage required).
func (a *StreamAnalyzer) AnalyzePosts(ctx context.Context, duration time.Duration, dimension string) (*models.AnalysisResult, error) {
	// Create context with timeout for the analysis duration
	analyzeCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	// Get stream results
	resultCh, err := a.streamClient.ReadEvents(analyzeCtx)
	if err != nil {
		return nil, err
	}

	// Incrementally compute aggregate metrics from posts (no storage) until either:
	// - The context timeout expires (after 'duration')
	// - The stream encounters an error (parse, scanner, network)
	// - The channel closes normally (unexpected, but handled)
	result, err := a.computeAnalysis(resultCh, dimension)

	// Return result with post collection error if one occurred
	if err != nil {
		return result, fmt.Errorf("partial results (analyzed %d posts): %w", result.TotalPosts, err)
	}

	return result, nil
}

// computeAnalysis computes analysis incrementally as posts arrive from the channel.
// Blocks until the channel closes.
// Memory usage: O(1) (only stores running totals, not the posts themselves)
func (a *StreamAnalyzer) computeAnalysis(resultCh <-chan StreamResult, dimension string) (*models.AnalysisResult, error) {
	// Create an aggregator (only stores statistics, not posts)
	aggregator := newAggregator(dimension)

	// Process each post as it arrives
	for result := range resultCh {
		// Handle stream error
		if result.Err != nil {
			a.logger.Error("Stream error during analysis", "err", result.Err, "posts_processed", aggregator.totalPosts)
			return aggregator.getResult(), result.Err
		}

		// Process valid post incrementally
		if result.Post != nil {
			aggregator.processPost(result.Post)
		}
	}

	// Return final computed result
	return aggregator.getResult(), nil
}
