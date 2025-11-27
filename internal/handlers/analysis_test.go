package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"upfluence-stream-analyzer/internal/models"
	"upfluence-stream-analyzer/internal/services"
)

// mockAnalyzerService is a mock implementation of the Analyzer Service for testing
type mockAnalyzerService struct {
	analyzePostsFn func(ctx context.Context, duration time.Duration, dimension string) (*models.AnalysisResult, error)
}

// Check interface implementation at compile-time
var _ services.AnalyzerService = &mockAnalyzerService{}

func (m *mockAnalyzerService) AnalyzePosts(ctx context.Context, duration time.Duration, dimension string) (*models.AnalysisResult, error) {
	if m.analyzePostsFn != nil {
		return m.analyzePostsFn(ctx, duration, dimension)
	}
	return &models.AnalysisResult{}, nil
}

// testLogger creates a logger that discards output for testing
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStreamAnalysisHandler_ParseParams(t *testing.T) {
	tests := []struct {
		name              string
		queryParams       string
		isError           bool
		expectedDuration  time.Duration
		expectedDimension string
		expectedErr       string
	}{
		{
			name:              "valid seconds duration",
			queryParams:       "duration=30s&dimension=likes",
			isError:           false,
			expectedDuration:  30 * time.Second,
			expectedDimension: "likes",
		},
		{
			name:              "valid minutes duration",
			queryParams:       "duration=5m&dimension=comments",
			isError:           false,
			expectedDuration:  5 * time.Minute,
			expectedDimension: "comments",
		},
		{
			name:              "valid hours duration",
			queryParams:       "duration=1h&dimension=favorites",
			isError:           false,
			expectedDuration:  1 * time.Hour,
			expectedDimension: "favorites",
		},
		{
			name:              "valid mixed duration",
			queryParams:       "duration=1h30m45s&dimension=retweets",
			isError:           false,
			expectedDuration:  1*time.Hour + 30*time.Minute + 45*time.Second,
			expectedDimension: "retweets",
		},
		{
			name:        "missing duration",
			queryParams: "dimension=likes",
			isError:     true,
			expectedErr: "missing required parameter: duration",
		},
		{
			name:        "invalid duration",
			queryParams: "duration=invalid&dimension=likes",
			isError:     true,
			expectedErr: "invalid duration format",
		},
		{
			name:        "negative duration",
			queryParams: "duration=-10s&dimension=likes",
			isError:     true,
			expectedErr: "duration must be positive",
		},
		{
			name:        "zero duration",
			queryParams: "duration=0&dimension=likes",
			isError:     true,
			expectedErr: "duration must be positive",
		},
		{
			name:        "missing dimension",
			queryParams: "duration=30s",
			isError:     true,
			expectedErr: "missing required parameter: dimension",
		},
		{
			name:        "invalid dimension",
			queryParams: "duration=30s&dimension=shares",
			isError:     true,
			expectedErr: "invalid dimension",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewStreamAnalysisHandler(&mockAnalyzerService{}, testLogger())

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/analysis?"+tc.queryParams, nil)

			// Extract query parameters from the request with parseParams
			duration, dimension, err := handler.parseParams(req)

			if tc.isError {
				if err == nil {
					t.Error("expected error but got nil")
				} else if tc.expectedErr != "" {
					errMessage := err.Error()
					if !strings.Contains(errMessage, tc.expectedErr) {
						t.Errorf("expected error to contain %q, got %q", tc.expectedErr, errMessage)
					}
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
				if duration != tc.expectedDuration {
					t.Errorf("expected duration %v, got %v", tc.expectedDuration, duration)
				}
				if dimension != tc.expectedDimension {
					t.Errorf("expected dimension %q, got %q", tc.expectedDimension, dimension)
				}
			}
		})
	}
}

func TestStreamAnalysisHandler_HandleAnalysis_ValidationErrors(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		expectedErr    string
	}{
		{
			name:           "missing duration parameter",
			queryParams:    "dimension=likes",
			expectedStatus: http.StatusBadRequest,
			expectedErr:    "missing required parameter: duration",
		},
		{
			name:           "invalid duration format",
			queryParams:    "duration=invalid&dimension=likes",
			expectedStatus: http.StatusBadRequest,
			expectedErr:    "invalid duration format",
		},
		{
			name:           "negative duration",
			queryParams:    "duration=-30s&dimension=likes",
			expectedStatus: http.StatusBadRequest,
			expectedErr:    "duration must be positive",
		},
		{
			name:           "zero duration",
			queryParams:    "duration=0s&dimension=likes",
			expectedStatus: http.StatusBadRequest,
			expectedErr:    "duration must be positive",
		},
		{
			name:           "missing dimension parameter",
			queryParams:    "duration=30s",
			expectedStatus: http.StatusBadRequest,
			expectedErr:    "missing required parameter: dimension",
		},
		{
			name:           "invalid dimension",
			queryParams:    "duration=30s&dimension=invalid",
			expectedStatus: http.StatusBadRequest,
			expectedErr:    "invalid dimension",
		},
		{
			name:           "dimension with wrong case",
			queryParams:    "duration=30s&dimension=Likes",
			expectedStatus: http.StatusBadRequest,
			expectedErr:    "invalid dimension",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock service (should not be called for validation errors)
			mockStreamAnalyzer := &mockAnalyzerService{
				analyzePostsFn: func(ctx context.Context, duration time.Duration, dimension string) (*models.AnalysisResult, error) {
					t.Error("AnalyzePosts should not be called for validation errors")
					return nil, nil
				},
			}

			handler := NewStreamAnalysisHandler(mockStreamAnalyzer, testLogger())

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/analysis?"+tc.queryParams, nil)

			// Execute request
			w := httptest.NewRecorder()
			handler.HandleAnalysis(w, req)

			// Assert status code
			if w.Code != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, w.Code)
			}

			// Parse error response
			var body map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to parse error response: %v", err)
			}

			// Assert error field is present
			errMessage, ok := body["error"].(string)
			if !ok {
				t.Fatalf("expected error field in response")
			}

			// Check if error message contains expected text
			if !strings.Contains(errMessage, tc.expectedErr) {
				t.Errorf("expected error to contain %q, got %q", tc.expectedErr, errMessage)
			}
		})
	}
}

func TestStreamAnalysisHandler_HandleAnalysis_MethodNotAllowed(t *testing.T) {
	methods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
	}

	for _, method := range methods {
		t.Run("method_"+method, func(t *testing.T) {
			// Setup mock service
			mockStreamAnalyzer := &mockAnalyzerService{
				analyzePostsFn: func(ctx context.Context, duration time.Duration, dimension string) (*models.AnalysisResult, error) {
					t.Error("AnalyzePosts should not be called for wrong HTTP method")
					return nil, nil
				},
			}

			handler := NewStreamAnalysisHandler(mockStreamAnalyzer, testLogger())

			// Create request with wrong method
			req := httptest.NewRequest(method, "/analysis?duration=30s&dimension=likes", nil)

			// Execute request
			w := httptest.NewRecorder()
			handler.HandleAnalysis(w, req)

			// Assert status code
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
			}

			// Parse error response
			var body map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to parse error response: %v", err)
			}

			// Assert error field is present
			errMessage, ok := body["error"].(string)
			if !ok {
				t.Fatalf("expected error field in response")
			}

			// Assert error message
			if errMessage != "only GET method is allowed" {
				t.Errorf("expected error 'only GET method is allowed', got %q", errMessage)
			}
		})
	}
}

func TestStreamAnalysisHandler_HandleAnalysis_ServiceError(t *testing.T) {
	tests := []struct {
		name           string
		serviceErr     error
		expectedStatus int
	}{
		{
			name:           "service returns generic error",
			serviceErr:     errors.New("stream connection failed"),
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "service returns context canceled",
			serviceErr:     context.Canceled,
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name:           "service returns context deadline exceeded",
			serviceErr:     context.DeadlineExceeded,
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock service that returns an error
			mockStreamAnalyzer := &mockAnalyzerService{
				analyzePostsFn: func(ctx context.Context, duration time.Duration, dimension string) (*models.AnalysisResult, error) {
					return nil, tc.serviceErr
				},
			}

			handler := NewStreamAnalysisHandler(mockStreamAnalyzer, testLogger())

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/analysis?duration=30s&dimension=likes", nil)

			// Execute request
			w := httptest.NewRecorder()
			handler.HandleAnalysis(w, req)

			// Assert status code
			if w.Code != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, w.Code)
			}

			// Parse error response
			var body map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to parse error response: %v", err)
			}

			// Assert error field is present
			errMessage, ok := body["error"].(string)
			if !ok {
				t.Fatalf("expected error field in response")
			}

			// Assert error message
			if errMessage != "failed to analyze stream" {
				t.Errorf("expected error 'failed to analyze stream', got %q", errMessage)
			}
		})
	}
}

func TestStreamAnalysisHandler_HandleAnalysis_Success(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    string
		mockResult     *models.AnalysisResult
		expectedStatus int
	}{
		{
			name:        "successful analysis with likes dimension",
			queryParams: "duration=30s&dimension=likes",
			mockResult: &models.AnalysisResult{
				TotalPosts:       10,
				MinimumTimestamp: 1554324856,
				MaximumTimestamp: 1633974046,
				Average:          1824,
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "successful analysis with comments dimension",
			queryParams: "duration=1m&dimension=comments",
			mockResult: &models.AnalysisResult{
				TotalPosts:       20,
				MinimumTimestamp: 1554324856,
				MaximumTimestamp: 1633974046,
				Average:          12740,
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "successful analysis with favorites dimension",
			queryParams: "duration=5s&dimension=favorites",
			mockResult: &models.AnalysisResult{
				TotalPosts:       30,
				MinimumTimestamp: 1554324856,
				MaximumTimestamp: 1633974046,
				Average:          203863,
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "successful analysis with retweets dimension",
			queryParams: "duration=10s&dimension=retweets",
			mockResult: &models.AnalysisResult{
				TotalPosts:       40,
				MinimumTimestamp: 1554324856,
				MaximumTimestamp: 1633974046,
				Average:          4207,
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "successful analysis with zero posts",
			queryParams: "duration=30s&dimension=likes",
			mockResult: &models.AnalysisResult{
				TotalPosts:       0,
				MinimumTimestamp: 0,
				MaximumTimestamp: 0,
				Average:          0,
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock service
			mockStreamAnalyzer := &mockAnalyzerService{
				analyzePostsFn: func(ctx context.Context, duration time.Duration, dimension string) (*models.AnalysisResult, error) {
					return tc.mockResult, nil
				},
			}

			handler := NewStreamAnalysisHandler(mockStreamAnalyzer, testLogger())

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/analysis?"+tc.queryParams, nil)

			// Execute request
			w := httptest.NewRecorder()
			handler.HandleAnalysis(w, req)

			// Assert status code
			if w.Code != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, w.Code)
			}

			// Assert Content-Type header
			if w.Header().Get("Content-Type") != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
			}

			// Parse response body
			var body map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to parse response body: %v", err)
			}

			// Extract dimension from the request with parseParams
			_, dimension, err := handler.parseParams(req)
			if err != nil {
				t.Fatalf("failed to parse params: %v", err)
			}

			// Assert response fields using mockResult values
			if body["total_posts"] != float64(tc.mockResult.TotalPosts) {
				t.Errorf("expected total_posts=%d, got %v", tc.mockResult.TotalPosts, body["total_posts"])
			}
			if body["minimum_timestamp"] != float64(tc.mockResult.MinimumTimestamp) {
				t.Errorf("expected minimum_timestamp=%d, got %v", tc.mockResult.MinimumTimestamp, body["minimum_timestamp"])
			}
			if body["maximum_timestamp"] != float64(tc.mockResult.MaximumTimestamp) {
				t.Errorf("expected maximum_timestamp=%d, got %v", tc.mockResult.MaximumTimestamp, body["maximum_timestamp"])
			}

			// Check the appropriate average field based on dimension
			avgKey := "avg_" + dimension
			if body[avgKey] != float64(tc.mockResult.Average) {
				t.Errorf("expected %s=%d, got %v", avgKey, tc.mockResult.Average, body[avgKey])
			}
		})
	}
}
