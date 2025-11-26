package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"upfluence-stream-analyzer/internal/models"
	"upfluence-stream-analyzer/internal/services"
)

// StreamAnalysisHandler handles HTTP requests for stream analysis
type StreamAnalysisHandler struct {
	streamAnalyzer services.AnalyzerService
	logger         *slog.Logger
}

// NewStreamAnalysisHandler creates a new analysis request handler
func NewStreamAnalysisHandler(streamAnalyzer services.AnalyzerService, logger *slog.Logger) *StreamAnalysisHandler {
	return &StreamAnalysisHandler{
		streamAnalyzer: streamAnalyzer,
		logger:         logger,
	}
}

// HandleAnalysis processes GET requests to '/analysis' endpoint
func (h *StreamAnalysisHandler) HandleAnalysis(w http.ResponseWriter, r *http.Request) {
	// Enforce GET method only
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "only GET method is allowed")
		return
	}

	// Parse and validate query parameters
	duration, dimension, err := h.parseParams(r)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.logger.Info("Analysis request started", "duration", duration, "dimension", dimension)

	// Perform analysis on posts (this blocks for the duration)
	ctx := r.Context()
	result, err := h.streamAnalyzer.AnalyzePosts(ctx, duration, dimension)
	if err != nil {
		h.logger.Error("Failed to perform analysis on posts", "err", err.Error())
		h.sendError(w, http.StatusInternalServerError, "failed to analyze stream")
		return
	}

	h.logger.Info("Analysis completed successfully", "total_posts", result.TotalPosts, "duration", duration, "dimension", dimension)

	// Send response
	h.sendResponse(w, dimension, result)
}

// parseParams extracts and validates query parameters
func (h *StreamAnalysisHandler) parseParams(r *http.Request) (time.Duration, string, error) {
	query := r.URL.Query()

	// Parse duration parameter
	durationStr := query.Get("duration")
	if durationStr == "" {
		return 0, "", fmt.Errorf("missing required parameter: duration")
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return 0, "", fmt.Errorf("invalid duration format: %s (expected format: 5s, 10m, 1h)", durationStr)
	}

	// Validate duration is positive
	if duration <= 0 {
		return 0, "", fmt.Errorf("duration must be positive")
	}

	// Parse dimension parameter
	dimension := query.Get("dimension")
	if dimension == "" {
		return 0, "", fmt.Errorf("missing required parameter: dimension")
	}

	// Validate dimension
	if !models.ValidDimensions[dimension] {
		return 0, "", fmt.Errorf("invalid dimension: %s (must be one of: likes, comments, favorites, retweets)", dimension)
	}

	return duration, dimension, nil
}

// sendResponse sends a successful JSON response
func (h *StreamAnalysisHandler) sendResponse(w http.ResponseWriter, dimension string, result *models.AnalysisResult) {
	// Build response with dynamic field name for average
	resp := map[string]interface{}{
		"total_posts":                    result.TotalPosts,
		"minimum_timestamp":              result.MinimumTimestamp,
		"maximum_timestamp":              result.MaximumTimestamp,
		fmt.Sprintf("avg_%s", dimension): result.Average,
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		h.logger.Error("Failed to marshal result response", "err", err.Error())
		h.sendError(w, http.StatusInternalServerError, "failed to encode result response")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(respBytes); err != nil {
		h.logger.Error("Failed to write result response", "err", err.Error())
	}
}

// sendError sends an error response with appropriate status code
func (h *StreamAnalysisHandler) sendError(w http.ResponseWriter, statusCode int, message string) {
	resp := map[string]string{
		"error": message,
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		h.logger.Error("Failed to marshal error response", "err", err.Error())
		http.Error(w, message, statusCode)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if _, err := w.Write(respBytes); err != nil {
		h.logger.Error("Failed to write error response", "err", err.Error())
	}
}
