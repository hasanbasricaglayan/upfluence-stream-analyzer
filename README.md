# Upfluence Stream Analyzer
An HTTP API server that consumes real-time social media posts from Upfluence's SSE (Server-Sent Events) stream and produces statistical aggregations on them over configurable time windows.


## Overview
This application connects to Upfluence's public SSE endpoint, collects and computes aggregate statistics (total count, timestamp range, and dimensional averages) on social media posts for a specified duration.

**Key Features:**
- Real-time SSE stream consumption with graceful error handling
- Time-bounded analysis with configurable duration
- Multi-dimensional analysis (likes, comments, favorites, retweets)
- Production-ready logging and error handling
- Context-aware cancellation propagation
- Graceful shutdown with proper resource cleanup


## Technical Architecture
The application follows a **service-oriented architecture (SOA)** with clear separation of concerns:

### 1. **Handler Layer** (`internal/handlers`)
- HTTP request/response handling
- Query parameter validation
- Error response formatting
- Timeout management

### 2. **Service Layer** (`internal/services`)
- **StreamClient**: Manages SSE connection lifecycle
  - Establishes HTTP connection to stream
  - Parses SSE events line-by-line
  - Streams parsed posts through result channel
  - Handles context cancellation and errors

- **StreamAnalyzer**: Performs statistical analysis
  - Collects posts from result channel
  - Computes aggregate metrics
  - Handles edge cases (empty results, missing dimensions)

### 3. **Model Layer** (`internal/models`)
- Post payload structures
- Dimension validation
- Type-safe parsing logic

### 4. **Configuration** (`config`)
- JSON-based configuration
- Configuration validation


## Technical Choices
### 1. **Go Standard Library Only**
Following the challenge requirements, the implementation uses only Go's standard library for:
- HTTP server (`net/http`)
- SSE parsing (`bufio.Scanner`)
- JSON encoding/decoding (`encoding/json`)
- Context management (`context`)
- Structured logging (`log/slog`)

### 2. **Interface-Based Design**
Key components expose interfaces (`StreamService`, `AnalyzerService`) rather than concrete types.

**Benefits:**
- Easy to mock for testing
- Dependency injection friendly
- Loose coupling between layers
- Facilitates future extensions

### 3. **Channel-Based Architecture**
Posts flow through a buffered channel (`chan StreamResult`) from the stream reader to the analyzer.

**Benefits:**
- Decouples stream reading from processing (asynchronous communication)
- Natural backpressure mechanism (buffered channel of 100)
- Goroutine-safe communication
- Easy to test independently

**Trade-off**: Channel buffer size (100) is a tuning parameter. Too small causes blocking; too large increases memory usage.

### 4. **Efficient Memory Usage**
Statistics are computed on-the-fly without storing posts in memory.

Memory usage: O(1) instead of O(N), where N = number of analyzed posts

### 5. **Error Handling Strategy**
- **Connection errors**: Fail fast, return immediately
- **Context cancellation**: Expected behavior, not an error
- **Stream and parse errors**: Propagated through result channel, return with partial results

This allows clients to make informed decisions about partial data.

### 6. **Context-Driven Cancellation**
Uses Go's `context.Context` throughout for timeout and cancellation propagation.
When duration expires or shutdown occurs, cancellation propagates automatically through all layers.

### 7. **Graceful Shutdown with BaseContext**
The HTTP server uses `BaseContext` to propagate shutdown signals to all active requests.

**Flow:**
1. OS signal received (`SIGINT`/`SIGTERM`)
2. Base context cancelled -> All request contexts cancelled
3. In-flight requests detect cancellation and clean up
4. Server waits up to 30s for requests to complete
5. Clean shutdown

This ensures no requests are abruptly terminated and stream connections are properly closed.

### 8. **Structured Logging**
Uses `log/slog` (Go 1.21+) for structured logging.


## Trade-offs & Design Decisions
### 1. **Blocking Request Model**
The API blocks for the entire `duration` parameter.

**Pros:**
- Simple client interaction (single request/response)
- No state management needed
- Easy to understand and debug

**Cons:**
- Ties up server resources during analysis
- Not suitable for very long durations

### 2. **Single-Threaded Analysis**
Each request is independent (no shared state) and processes posts sequentially in a single goroutine.

**Scalability:** The HTTP server handles multiple concurrent requests naturally through Go's goroutine-per-request model.

### 3. **Channel Buffer Size (100)**
Buffer prevents blocking on temporary spikes.

**Could adjust if:**
- Stream throughput increases significantly
- Want more aggressive backpressure
- Memory constraints require smaller buffer

### 4. **No Request ID / Distributed Tracing**
Currently, logs use component-level context but not request-specific IDs.

**Left out for simplicity, but production would add:**
- Request ID middleware
- Correlation IDs in logs

### 5. **Error Response Format**
Simple JSON format sufficient for this use case: `{"error": "message"}`

**Alternative considered:** Structured errors with codes
```json
{
  "error": {
    "code": "INVALID_DURATION",
    "message": "...",
    "details": "{...}"
  }
}
```


## What I Would Do Differently With More Time
### 1. **Channel Backpressure Handling**
Currently, the stream client uses a fixed buffer size (100) for the result channel.
Under high post throughput and/or slow post analysis, this buffer could fill up, causing the stream reader to block. This could lead to:
- Temporary blocking when buffer is full
- Potential data loss if stream continues sending
- Degraded performance under spike loads

**Potential solutions:**
- **Metrics**: Monitor channel utilization to detect bottlenecks
- **Dynamic buffer sizing**: Adjust buffer based on observed throughput
- **Fan-Out/Fan-In Pattern**: Multiple worker goroutines process posts in parallel
  ```
  SSE Stream -> StreamClient -> Fan-Out -> [Worker1, Worker2, Worker3] -> Fan-In -> Analyzer
  ```

This is particularly important for production deployments expecting high-volume streams or running on resource-constrained environments.

### 2. **Circuit Breaker**
If Upfluence's SSE endpoint is down, implement:
- Retry with exponential backoff
- Circuit breaker pattern
- Fallback responses


## Running the Project
### Prerequisites
- **Go 1.21+** (requires `log/slog` from standard library)
- Internet connection (to reach Upfluence Stream API)
- A `config.json` file (see Configuration section below)

### Configuration
The application uses a JSON-based configuration file for settings.

**Default configuration file: `config.json`**
```json
{
	"stream": {
		"url": "UpfluenceStreamURL"
	},
	"server": {
		"host": "localhost",
		"port": 8080
	}
}
```

**Configuration fields:**
- `stream.url` - URL of the Upfluence SSE stream endpoint
- `server.host` - Host address for the HTTP server (default: `localhost`)
- `server.port` - Port number for the HTTP server (default: `8080`)

**Setup:**
The repository includes a `config.example.json` file as a template in `config`. Copy it to create your own `config.json` in `config` folder. Do not forget to set the `stream.url` field.

**Note:** `config.json` is gitignored to prevent accidentally committing sensitive or environment-specific settings. Always use `config.example.json` as the template.


### Running the Server
```bash
# Run directly
go run cmd/main.go

# Or build and run
go build -o stream-analyzer cmd/main.go
./stream-analyzer
```

### Testing the API
#### Basic Request
```bash
curl "http://localhost:8080/analysis?duration=30s&dimension=likes"
```

Expected response (after 30 seconds):
```json
{
  "total_posts": 42,
  "minimum_timestamp": 1705315800,
  "maximum_timestamp": 1705315830,
  "avg_likes": 128
}
```

#### Try Different Dimensions
```bash
# Analyze comments
curl "http://localhost:8080/analysis?duration=10s&dimension=comments"

# Analyze retweets
curl "http://localhost:8080/analysis?duration=1m&dimension=retweets"

# Analyze favorites
curl "http://localhost:8080/analysis?duration=45s&dimension=favorites"
```

### Graceful Shutdown
The server handles shutdown signals gracefully:
```bash
# Start the server
./stream-analyzer

# In another terminal, make a long request
curl "http://localhost:8080/analysis?duration=60s&dimension=likes"

# Press Ctrl+C in the server terminal
# Server will:
# 1. Stop accepting new connections
# 2. Cancel all active request contexts
# 3. Wait up to 30s for in-flight requests to complete
# 4. Clean up and exit
```
