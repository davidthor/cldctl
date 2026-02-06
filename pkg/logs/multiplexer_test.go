package logs

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFormatQueryResult_Empty(t *testing.T) {
	var buf bytes.Buffer
	FormatQueryResult(&buf, &QueryResult{}, MultiplexOptions{})
	if buf.Len() != 0 {
		t.Errorf("expected empty output for empty result, got %q", buf.String())
	}
}

func TestFormatQueryResult_SingleEntry(t *testing.T) {
	var buf bytes.Buffer
	result := &QueryResult{
		Entries: []LogEntry{
			{
				Timestamp: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
				Line:      "Server started",
				Labels: map[string]string{
					"service_namespace": "my-app",
					"service_type":      "deployment",
					"service_name":      "my-app-api",
				},
			},
		},
	}

	FormatQueryResult(&buf, result, MultiplexOptions{NoColor: true})
	output := buf.String()

	if !strings.Contains(output, "my-app/deployment/api") {
		t.Errorf("expected label 'my-app/deployment/api' in output, got %q", output)
	}
	if !strings.Contains(output, "Server started") {
		t.Errorf("expected log line in output, got %q", output)
	}
	if !strings.Contains(output, " | ") {
		t.Errorf("expected separator ' | ' in output, got %q", output)
	}
}

func TestFormatQueryResult_SortsByTimestamp(t *testing.T) {
	var buf bytes.Buffer
	t1 := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 15, 10, 29, 0, 0, time.UTC) // Earlier
	result := &QueryResult{
		Entries: []LogEntry{
			{Timestamp: t1, Line: "Second", Labels: map[string]string{"service_name": "api"}},
			{Timestamp: t2, Line: "First", Labels: map[string]string{"service_name": "api"}},
		},
	}

	FormatQueryResult(&buf, result, MultiplexOptions{NoColor: true})
	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "First") {
		t.Errorf("expected first line to contain 'First', got %q", lines[0])
	}
	if !strings.Contains(lines[1], "Second") {
		t.Errorf("expected second line to contain 'Second', got %q", lines[1])
	}
}

func TestFormatQueryResult_WithTimestamps(t *testing.T) {
	var buf bytes.Buffer
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	result := &QueryResult{
		Entries: []LogEntry{
			{Timestamp: ts, Line: "Hello", Labels: map[string]string{"service_name": "api"}},
		},
	}

	FormatQueryResult(&buf, result, MultiplexOptions{NoColor: true, ShowTimestamps: true})
	output := buf.String()

	if !strings.Contains(output, "2025-01-15T10:30:00.000Z") {
		t.Errorf("expected timestamp in output, got %q", output)
	}
}

func TestFormatQueryResult_WithColor(t *testing.T) {
	var buf bytes.Buffer
	result := &QueryResult{
		Entries: []LogEntry{
			{
				Timestamp: time.Now(),
				Line:      "Log line",
				Labels: map[string]string{
					"service_namespace": "my-app",
					"service_name":      "my-app-api",
				},
			},
		},
	}

	FormatQueryResult(&buf, result, MultiplexOptions{NoColor: false})
	output := buf.String()

	// Should contain ANSI color codes
	if !strings.Contains(output, "\033[") {
		t.Errorf("expected ANSI color codes in output, got %q", output)
	}
}

func TestFormatQueryResult_MultipleWorkloads(t *testing.T) {
	var buf bytes.Buffer
	ts := time.Now()
	result := &QueryResult{
		Entries: []LogEntry{
			{Timestamp: ts, Line: "API log", Labels: map[string]string{
				"service_namespace": "my-app", "service_type": "deployment", "service_name": "my-app-api",
			}},
			{Timestamp: ts.Add(time.Second), Line: "Worker log", Labels: map[string]string{
				"service_namespace": "my-app", "service_type": "deployment", "service_name": "my-app-worker",
			}},
		},
	}

	FormatQueryResult(&buf, result, MultiplexOptions{NoColor: true})
	output := buf.String()

	if !strings.Contains(output, "my-app/deployment/api") {
		t.Errorf("expected 'my-app/deployment/api' label, got %q", output)
	}
	if !strings.Contains(output, "my-app/deployment/worker") {
		t.Errorf("expected 'my-app/deployment/worker' label, got %q", output)
	}
}

func TestEntryLabel(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name:   "all fields",
			labels: map[string]string{"service_namespace": "my-app", "service_type": "deployment", "service_name": "my-app-api"},
			want:   "my-app/deployment/api",
		},
		{
			name:   "namespace and name without type",
			labels: map[string]string{"service_namespace": "my-app", "service_name": "my-app-api"},
			want:   "my-app/api",
		},
		{
			name:   "namespace and type only",
			labels: map[string]string{"service_namespace": "my-app", "service_type": "deployment"},
			want:   "my-app/deployment",
		},
		{
			name:   "namespace only",
			labels: map[string]string{"service_namespace": "my-app"},
			want:   "my-app",
		},
		{
			name:   "name only",
			labels: map[string]string{"service_name": "some-service"},
			want:   "some-service",
		},
		{
			name:   "neither",
			labels: map[string]string{},
			want:   "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := LogEntry{Labels: tt.labels}
			got := entryLabel(entry)
			if got != tt.want {
				t.Errorf("entryLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildColorMap_NoColor(t *testing.T) {
	entries := []LogEntry{
		{Labels: map[string]string{"service_name": "api"}},
	}
	cm := buildColorMap(entries, true)
	if len(cm) != 0 {
		t.Errorf("expected empty color map with noColor=true, got %v", cm)
	}
}

func TestBuildColorMap_AssignsColors(t *testing.T) {
	entries := []LogEntry{
		{Labels: map[string]string{"service_name": "api"}},
		{Labels: map[string]string{"service_name": "worker"}},
		{Labels: map[string]string{"service_name": "api"}}, // duplicate
	}
	cm := buildColorMap(entries, false)
	if len(cm) != 2 {
		t.Errorf("expected 2 colors, got %d", len(cm))
	}
	if cm["api"] == cm["worker"] {
		t.Error("expected different colors for different labels")
	}
}
