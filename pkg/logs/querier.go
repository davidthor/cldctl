// Package logs provides a pluggable log querying framework.
//
// Log adapters (e.g., Loki, CloudWatch) register themselves via init() in their
// sub-packages. The CLI imports these packages as side effects to make them
// available at runtime.
package logs

import (
	"context"
	"time"
)

// LogQuerier is the interface that log query adapters must implement.
type LogQuerier interface {
	// Query retrieves historical log entries matching the given options.
	Query(ctx context.Context, opts QueryOptions) (*QueryResult, error)

	// Tail returns a live log stream matching the given options.
	Tail(ctx context.Context, opts QueryOptions) (*LogStream, error)
}

// LogStream represents a live log stream.
// Entries are delivered on the Entries channel. The Err channel receives
// any non-nil error that terminates the stream. Both channels are closed
// when the stream ends.
type LogStream struct {
	Entries <-chan LogEntry
	Err     <-chan error
	close   func()
}

// NewLogStream creates a LogStream backed by the provided channels and closer.
func NewLogStream(entries <-chan LogEntry, errs <-chan error, closer func()) *LogStream {
	return &LogStream{
		Entries: entries,
		Err:     errs,
		close:   closer,
	}
}

// Close terminates the stream and releases resources.
func (s *LogStream) Close() error {
	if s.close != nil {
		s.close()
	}
	return nil
}

// QueryOptions specifies filters for a log query.
type QueryOptions struct {
	Environment  string
	Component    string
	ResourceType string
	Workload     string
	Since        time.Time
	Limit        int
}

// QueryResult contains the results of a historical log query.
type QueryResult struct {
	Entries []LogEntry
}

// LogEntry represents a single log line with labels and a timestamp.
type LogEntry struct {
	Timestamp time.Time
	Line      string
	Labels    map[string]string
}
