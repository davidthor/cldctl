// Package loki provides a Loki-based log query adapter for arcctl.
//
// It is imported as a side effect to register the "loki" query type:
//
//	import _ "github.com/architect-io/arcctl/pkg/logs/loki"
package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/architect-io/arcctl/pkg/logs"
	"github.com/gorilla/websocket"
)

func init() {
	logs.Register("loki", func(endpoint string) (logs.LogQuerier, error) {
		return New(endpoint)
	})
}

// Querier implements logs.LogQuerier against a Loki HTTP API.
type Querier struct {
	endpoint string
	client   *http.Client
}

// New creates a Loki querier pointed at the given base URL
// (e.g. "http://localhost:3100").
func New(endpoint string) (logs.LogQuerier, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return nil, fmt.Errorf("loki endpoint must not be empty")
	}
	return &Querier{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// ---------------------------------------------------------------------------
// Query (historical)
// ---------------------------------------------------------------------------

// Query retrieves historical log entries from Loki.
func (q *Querier) Query(ctx context.Context, opts logs.QueryOptions) (*logs.QueryResult, error) {
	logQL := buildLogQL(opts)

	params := url.Values{}
	params.Set("query", logQL)
	params.Set("direction", "backward")

	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if !opts.Since.IsZero() {
		params.Set("start", strconv.FormatInt(opts.Since.UnixNano(), 10))
	}
	params.Set("end", strconv.FormatInt(time.Now().UnixNano(), 10))

	reqURL := fmt.Sprintf("%s/loki/api/v1/query_range?%s", q.endpoint, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := q.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loki query failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("loki returned %d: %s", resp.StatusCode, string(body))
	}

	var lokiResp queryRangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&lokiResp); err != nil {
		return nil, fmt.Errorf("failed to decode loki response: %w", err)
	}

	entries := make([]logs.LogEntry, 0)
	for _, stream := range lokiResp.Data.Result {
		for _, v := range stream.Values {
			entries = append(entries, parseValuePair(v, stream.Stream))
		}
	}

	// Sort oldest-first
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	return &logs.QueryResult{Entries: entries}, nil
}

// ---------------------------------------------------------------------------
// Tail (live streaming via WebSocket)
// ---------------------------------------------------------------------------

// Tail connects to Loki's /loki/api/v1/tail WebSocket endpoint and returns
// a LogStream that emits entries as they arrive.
func (q *Querier) Tail(ctx context.Context, opts logs.QueryOptions) (*logs.LogStream, error) {
	logQL := buildLogQL(opts)

	params := url.Values{}
	params.Set("query", logQL)
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if !opts.Since.IsZero() {
		params.Set("start", strconv.FormatInt(opts.Since.UnixNano(), 10))
	}

	// Build WebSocket URL (http→ws, https→wss)
	wsURL := strings.Replace(q.endpoint, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = fmt.Sprintf("%s/loki/api/v1/tail?%s", wsURL, params.Encode())

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to loki tail endpoint: %w", err)
	}

	entries := make(chan logs.LogEntry, 100)
	errs := make(chan error, 1)

	// Read loop in a goroutine
	go func() {
		defer close(entries)
		defer close(errs)
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					return
				}
				if ctx.Err() != nil {
					return
				}
				errs <- fmt.Errorf("loki tail read error: %w", err)
				return
			}

			var resp tailResponse
			if err := json.Unmarshal(msg, &resp); err != nil {
				errs <- fmt.Errorf("failed to decode tail message: %w", err)
				return
			}

			for _, stream := range resp.Streams {
				for _, v := range stream.Values {
					entry := parseValuePair(v, stream.Stream)
					select {
					case entries <- entry:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	closer := func() {
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		conn.Close()
	}

	return logs.NewLogStream(entries, errs, closer), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseValuePair converts a Loki [timestamp, line] pair into a LogEntry.
func parseValuePair(v []string, labels map[string]string) logs.LogEntry {
	ts := int64(0)
	line := ""
	if len(v) >= 1 {
		ts, _ = strconv.ParseInt(v[0], 10, 64)
	}
	if len(v) >= 2 {
		line = v[1]
	}

	// Copy labels so callers can't mutate the original map
	labelCopy := make(map[string]string, len(labels))
	for k, val := range labels {
		labelCopy[k] = val
	}

	return logs.LogEntry{
		Timestamp: time.Unix(0, ts),
		Line:      line,
		Labels:    labelCopy,
	}
}

// buildLogQL constructs a LogQL stream selector from query options.
func buildLogQL(opts logs.QueryOptions) string {
	selectors := make([]string, 0)

	if opts.Environment != "" {
		selectors = append(selectors, fmt.Sprintf(`deployment_environment=%q`, opts.Environment))
	}
	if opts.Component != "" {
		selectors = append(selectors, fmt.Sprintf(`service_namespace=%q`, opts.Component))
	}
	if opts.ResourceType != "" {
		selectors = append(selectors, fmt.Sprintf(`service_type=%q`, opts.ResourceType))
	}
	if opts.Workload != "" {
		selectors = append(selectors, fmt.Sprintf(`service_name=%q`, opts.Workload))
	}

	if len(selectors) == 0 {
		return `{job="arcctl"}`
	}
	return fmt.Sprintf(`{%s}`, strings.Join(selectors, ", "))
}

// ---------------------------------------------------------------------------
// Loki response types
// ---------------------------------------------------------------------------

type queryRangeResponse struct {
	Status string         `json:"status"`
	Data   queryRangeData `json:"data"`
}

type queryRangeData struct {
	ResultType string             `json:"resultType"`
	Result     []queryRangeStream `json:"result"`
}

type queryRangeStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

type tailResponse struct {
	Streams []tailStream `json:"streams"`
}

type tailStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}
