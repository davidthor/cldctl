package loki

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/davidthor/cldctl/pkg/logs"
	"github.com/gorilla/websocket"
)

func TestBuildLogQL_AllFields(t *testing.T) {
	opts := logs.QueryOptions{
		Environment:  "staging",
		Component:    "my-app",
		ResourceType: "deployment",
		Workload:     "my-app-api",
	}

	logQL := buildLogQL(opts)

	if logQL != `{deployment_environment="staging", service_namespace="my-app", service_type="deployment", service_name="my-app-api"}` {
		t.Errorf("unexpected LogQL: %s", logQL)
	}
}

func TestBuildLogQL_WithResourceType(t *testing.T) {
	opts := logs.QueryOptions{
		Environment:  "staging",
		Component:    "my-app",
		ResourceType: "database",
	}

	logQL := buildLogQL(opts)

	if logQL != `{deployment_environment="staging", service_namespace="my-app", service_type="database"}` {
		t.Errorf("unexpected LogQL: %s", logQL)
	}
}

func TestBuildLogQL_EnvironmentOnly(t *testing.T) {
	opts := logs.QueryOptions{
		Environment: "prod",
	}

	logQL := buildLogQL(opts)

	if logQL != `{deployment_environment="prod"}` {
		t.Errorf("unexpected LogQL: %s", logQL)
	}
}

func TestBuildLogQL_Empty(t *testing.T) {
	opts := logs.QueryOptions{}

	logQL := buildLogQL(opts)

	if logQL != `{job="cldctl"}` {
		t.Errorf("unexpected LogQL for empty opts: %s", logQL)
	}
}

func TestQuery_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loki/api/v1/query_range" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		query := r.URL.Query().Get("query")
		if query != `{deployment_environment="test-env"}` {
			t.Errorf("unexpected query param: %s", query)
		}

		resp := queryRangeResponse{
			Status: "success",
			Data: queryRangeData{
				ResultType: "streams",
				Result: []queryRangeStream{
					{
						Stream: map[string]string{
							"service_namespace":      "my-app",
							"service_name":           "my-app-api",
							"deployment_environment": "test-env",
						},
						Values: [][]string{
							{"1700000000000000000", "Server started on :8080"},
							{"1700000001000000000", "Connected to database"},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	q, err := New(ts.URL)
	if err != nil {
		t.Fatalf("failed to create querier: %v", err)
	}

	result, err := q.Query(context.Background(), logs.QueryOptions{
		Environment: "test-env",
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(result.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result.Entries))
	}

	if result.Entries[0].Line != "Server started on :8080" {
		t.Errorf("unexpected first line: %s", result.Entries[0].Line)
	}
	if result.Entries[0].Labels["service_namespace"] != "my-app" {
		t.Errorf("unexpected label: %v", result.Entries[0].Labels)
	}
	if result.Entries[0].Timestamp.Unix() != 1700000000 {
		t.Errorf("unexpected timestamp: %v", result.Entries[0].Timestamp)
	}
}

func TestQuery_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad query"))
	}))
	defer ts.Close()

	q, err := New(ts.URL)
	if err != nil {
		t.Fatalf("failed to create querier: %v", err)
	}

	_, err = q.Query(context.Background(), logs.QueryOptions{
		Environment: "test-env",
	})
	if err == nil {
		t.Fatal("expected error for bad response")
	}
}

func TestQuery_WithLimit(t *testing.T) {
	var capturedLimit string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedLimit = r.URL.Query().Get("limit")
		resp := queryRangeResponse{Status: "success", Data: queryRangeData{ResultType: "streams"}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	q, err := New(ts.URL)
	if err != nil {
		t.Fatalf("failed to create querier: %v", err)
	}

	_, _ = q.(*Querier).Query(context.Background(), logs.QueryOptions{
		Environment: "test-env",
		Limit:       50,
	})

	if capturedLimit != "50" {
		t.Errorf("expected limit=50, got %s", capturedLimit)
	}
}

func TestNew_EmptyEndpoint(t *testing.T) {
	_, err := New("")
	if err == nil {
		t.Error("expected error for empty endpoint")
	}
}

func TestNew_TrailingSlash(t *testing.T) {
	q, err := New("http://localhost:3100/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lq := q.(*Querier)
	if lq.endpoint != "http://localhost:3100" {
		t.Errorf("expected trailing slash stripped, got %s", lq.endpoint)
	}
}

func TestTail_StreamsEntries(t *testing.T) {
	upgrader := websocket.Upgrader{}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loki/api/v1/tail" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		// Send two batches
		msg1 := tailResponse{
			Streams: []tailStream{
				{
					Stream: map[string]string{
						"service_namespace":      "my-app",
						"service_name":           "my-app-api",
						"deployment_environment": "test-env",
					},
					Values: [][]string{
						{"1700000000000000000", "Log line 1"},
						{"1700000001000000000", "Log line 2"},
					},
				},
			},
		}

		data, _ := json.Marshal(msg1)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Small delay, then close
		time.Sleep(50 * time.Millisecond)
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}))
	defer ts.Close()

	q, err := New(ts.URL)
	if err != nil {
		t.Fatalf("failed to create querier: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := q.Tail(ctx, logs.QueryOptions{
		Environment: "test-env",
	})
	if err != nil {
		t.Fatalf("tail failed: %v", err)
	}
	defer stream.Close()

	var received []logs.LogEntry
	for entry := range stream.Entries {
		received = append(received, entry)
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(received))
	}

	if received[0].Line != "Log line 1" {
		t.Errorf("unexpected first line: %s", received[0].Line)
	}
	if received[1].Line != "Log line 2" {
		t.Errorf("unexpected second line: %s", received[1].Line)
	}
}

func TestParseValuePair(t *testing.T) {
	labels := map[string]string{"service_name": "my-app-api"}
	entry := parseValuePair([]string{"1700000000000000000", "test line"}, labels)

	if entry.Line != "test line" {
		t.Errorf("unexpected line: %s", entry.Line)
	}
	if entry.Timestamp.Unix() != 1700000000 {
		t.Errorf("unexpected timestamp: %v", entry.Timestamp)
	}
	if entry.Labels["service_name"] != "my-app-api" {
		t.Errorf("unexpected label: %v", entry.Labels)
	}

	// Verify labels were copied (not shared)
	labels["service_name"] = "mutated"
	if entry.Labels["service_name"] != "my-app-api" {
		t.Error("expected label copy, not reference")
	}
}

func TestRegistration(t *testing.T) {
	// The init() function should have registered "loki"
	q, err := logs.NewQuerier("loki", "http://localhost:3100")
	if err != nil {
		t.Fatalf("expected loki to be registered: %v", err)
	}
	if q == nil {
		t.Fatal("expected non-nil querier")
	}
}
