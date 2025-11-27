package models

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// PostPayload represents a social media post from Upfluence's SSE stream
type PostPayload struct {
	// The single root key is the type of the social media post
	Type string `json:"-"`

	// Post data
	Data Post
}

type Post struct {
	// The 'timestamp' key represents the creation date of the post
	Timestamp int64 `json:"timestamp"` // The 'func (Time) Unix' of Go's standard library returns an int64

	// Details about the post
	Details map[string]interface{} `json:"-"`
}

// ValidDimensions lists all supported dimension types
var ValidDimensions = map[string]bool{
	"likes":     true,
	"comments":  true,
	"favorites": true,
	"retweets":  true,
}

// UnmarshalJSON implements custom JSON unmarshalling for PostPayload.
// Handles the dynamic structure where the post type is the root key.
func (p *PostPayload) UnmarshalJSON(event []byte) error {
	var eventRaw map[string]json.RawMessage
	if err := json.Unmarshal(event, &eventRaw); err != nil {
		return fmt.Errorf("failed to unmarshal post payload: %w", err)
	}

	// Should have exactly one root key (the post type)
	if len(eventRaw) != 1 {
		return fmt.Errorf("expected single root key, got %d", len(eventRaw))
	}

	// Extract post type and post data
	for postType, postData := range eventRaw {
		p.Type = postType

		var postDetails map[string]interface{}
		if err := json.Unmarshal(postData, &postDetails); err != nil {
			return fmt.Errorf("failed to unmarshal %s data: %w", postType, err)
		}

		// Extract and validate post timestamp
		timestamp, err := extractTimestamp(postDetails)
		if err != nil {
			return err
		}

		p.Data.Timestamp = timestamp

		p.Data.Details = make(map[string]interface{})

		// Iterate over valid dimensions and track only dimension fields (for analysis)
		for dim := range ValidDimensions {
			// Check if dimension exists in post details
			if val, ok := postDetails[dim]; ok {
				p.Data.Details[dim] = val
			}
		}
	}

	return nil
}

// GetDimensionValue extracts the value of a specified dimension from the post
func (p *PostPayload) GetDimensionValue(dimension string) (uint64, bool) {
	val, ok := p.Data.Details[dimension]
	if !ok {
		return 0, false
	}

	// Convert to string for parsing
	valStr := fmt.Sprintf("%v", val)

	// Parse as uint64.
	// An uint32 may not be sufficient...
	valInt, err := strconv.ParseUint(valStr, 10, 64)
	if err != nil {
		return 0, false
	}

	return valInt, true
}

func extractTimestamp(postDetails map[string]interface{}) (int64, error) {
	tsRaw, ok := postDetails["timestamp"]
	if !ok {
		return 0, fmt.Errorf("missing timestamp field")
	}

	// Convert to string for parsing.
	// When Unmarshal is done into an interface value, Unmarshal stores numbers as float64.
	tsStr := fmt.Sprintf("%.0f", tsRaw.(float64))

	// Parse as int64
	tsUnix, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid timestamp format: %w", err)
	}

	// Validate timestamp is positive
	if tsUnix <= 0 {
		return 0, fmt.Errorf("timestamp must be positive, got %d", tsUnix)
	}

	return tsUnix, nil
}
