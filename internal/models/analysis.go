package models

// AnalysisResult represents the output of a stream analysis
type AnalysisResult struct {
	TotalPosts       int   `json:"total_posts"`
	MinimumTimestamp int64 `json:"minimum_timestamp"`
	MaximumTimestamp int64 `json:"maximum_timestamp"`
	Average          int   `json:"-"`
}
