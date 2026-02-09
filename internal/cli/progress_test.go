package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewProgressTable(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	assert.NotNil(t, pt)
	assert.NotNil(t, pt.resources)
	assert.Equal(t, 0, len(pt.order))
}

func TestProgressTable_AddResource(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/database/main", "main", "database", "comp", nil)

	assert.Equal(t, 1, len(pt.resources))
	assert.Equal(t, "main", pt.resources["comp/database/main"].Name)
	assert.Equal(t, "database", pt.resources["comp/database/main"].Type)
	assert.Equal(t, StatusPending, pt.resources["comp/database/main"].Status)
}

func TestProgressTable_AddResourceWithDependencies(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/database/main", "main", "database", "comp", nil)
	pt.AddResource("comp/deployment/api", "api", "deployment", "comp", []string{"comp/database/main"})

	assert.Equal(t, 2, len(pt.resources))
	// Resources with dependencies start in "waiting" status
	assert.Equal(t, StatusWaiting, pt.resources["comp/deployment/api"].Status)
	assert.Equal(t, []string{"comp/database/main"}, pt.resources["comp/deployment/api"].Dependencies)
}

func TestProgressTable_UpdateStatus(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/database/main", "main", "database", "comp", nil)
	pt.UpdateStatus("comp/database/main", StatusInProgress, "provisioning")

	assert.Equal(t, StatusInProgress, pt.resources["comp/database/main"].Status)
	assert.Equal(t, "provisioning", pt.resources["comp/database/main"].Message)
	assert.False(t, pt.resources["comp/database/main"].StartTime.IsZero())
}

func TestProgressTable_SetError(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/database/main", "main", "database", "comp", nil)
	pt.SetError("comp/database/main", assert.AnError)

	assert.Equal(t, StatusFailed, pt.resources["comp/database/main"].Status)
	assert.Equal(t, assert.AnError, pt.resources["comp/database/main"].Error)
}

func TestProgressTable_SetInferredConfig(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/function/app", "app", "function", "comp", nil)

	config := map[string]string{
		"language":        "javascript",
		"framework":       "nextjs",
		"install_command": "npm install",
		"dev_command":     "npm run dev",
		"port":            "3000",
	}
	pt.SetInferredConfig("comp/function/app", config)

	assert.Equal(t, config, pt.resources["comp/function/app"].InferredConfig)
}

func TestProgressTable_SetLogs(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/function/app", "app", "function", "comp", nil)
	pt.SetLogs("comp/function/app", "npm ERR! code ENOENT\nnpm ERR! missing script: dev")

	assert.Equal(t, "npm ERR! code ENOENT\nnpm ERR! missing script: dev", pt.resources["comp/function/app"].Logs)
}

func TestProgressTable_AppendLogs(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/function/app", "app", "function", "comp", nil)
	pt.SetLogs("comp/function/app", "Line 1\n")
	pt.AppendLogs("comp/function/app", "Line 2\n")

	assert.Equal(t, "Line 1\nLine 2\n", pt.resources["comp/function/app"].Logs)
}

func TestProgressTable_GetCompletedCount(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/database/main", "main", "database", "comp", nil)
	pt.AddResource("comp/database/cache", "cache", "database", "comp", nil)
	pt.UpdateStatus("comp/database/main", StatusCompleted, "")

	assert.Equal(t, 1, pt.GetCompletedCount())
}

func TestProgressTable_GetFailedCount(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/database/main", "main", "database", "comp", nil)
	pt.AddResource("comp/database/cache", "cache", "database", "comp", nil)
	pt.SetError("comp/database/main", assert.AnError)

	assert.Equal(t, 1, pt.GetFailedCount())
}

func TestProgressTable_HasPending(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/database/main", "main", "database", "comp", nil)
	assert.True(t, pt.HasPending())

	pt.UpdateStatus("comp/database/main", StatusCompleted, "")
	assert.False(t, pt.HasPending())
}

func TestProgressTable_CheckDependencies(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/database/main", "main", "database", "comp", nil)
	pt.AddResource("comp/deployment/api", "api", "deployment", "comp", []string{"comp/database/main"})

	// api should be waiting, not ready
	assert.Equal(t, StatusWaiting, pt.resources["comp/deployment/api"].Status)
	ready := pt.CheckDependencies()
	assert.Empty(t, ready)

	// After database completes, api should become pending
	pt.UpdateStatus("comp/database/main", StatusCompleted, "")
	ready = pt.CheckDependencies()
	assert.Contains(t, ready, "comp/deployment/api")
	assert.Equal(t, StatusPending, pt.resources["comp/deployment/api"].Status)
}

func TestProgressTable_PrintInitialPlan(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/database/main", "main", "database", "comp", nil)
	pt.AddResource("comp/bucket/uploads", "uploads", "bucket", "comp", nil)
	pt.AddResource("comp/task/migration", "migration", "task", "comp", []string{"comp/database/main"})
	pt.AddResource("comp/deployment/api", "api", "deployment", "comp", []string{"comp/database/main", "comp/bucket/uploads", "comp/task/migration"})
	pt.PrintInitial()

	output := buf.String()
	assert.Contains(t, output, "Deployment Plan")
	assert.Contains(t, output, "main")
	assert.Contains(t, output, "api")
	// Verify full type/name dependency format
	assert.Contains(t, output, "depends on: database/main, bucket/uploads, task/migration")
}

func TestProgressTable_PrintFinalSummary_Success(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/database/main", "main", "database", "comp", nil)
	pt.UpdateStatus("comp/database/main", StatusCompleted, "")
	pt.PrintFinalSummary()

	output := buf.String()
	assert.Contains(t, output, "successfully")
	assert.Contains(t, output, "1 resources deployed")
}

func TestProgressTable_PrintFinalSummary_WithFailures(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/database/main", "main", "database", "comp", nil)
	pt.AddResource("comp/deployment/api", "api", "deployment", "comp", nil)
	pt.UpdateStatus("comp/database/main", StatusCompleted, "")
	pt.SetError("comp/deployment/api", assert.AnError)
	pt.PrintFinalSummary()

	output := buf.String()
	assert.Contains(t, output, "FAILED")
	assert.Contains(t, output, "1 succeeded")
	assert.Contains(t, output, "1 failed")
	assert.Contains(t, output, "Errors:")
	assert.Contains(t, output, "deployment/api")
}

func TestProgressTable_PrintFinalSummary_WithInferredConfig(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/function/app", "app", "function", "comp", nil)

	// Set inferred configuration
	config := map[string]string{
		"language":        "javascript",
		"framework":       "nextjs",
		"install_command": "npm install",
		"dev_command":     "npm run dev",
		"port":            "3000",
	}
	pt.SetInferredConfig("comp/function/app", config)
	pt.SetError("comp/function/app", assert.AnError)
	pt.PrintFinalSummary()

	output := buf.String()
	assert.Contains(t, output, "Inferred configuration")
	assert.Contains(t, output, "language: javascript")
	assert.Contains(t, output, "framework: nextjs")
	assert.Contains(t, output, "install_command: npm install")
	assert.Contains(t, output, "dev_command: npm run dev")
	assert.Contains(t, output, "port: 3000")
}

func TestProgressTable_PrintFinalSummary_WithLogs(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/function/app", "app", "function", "comp", nil)

	// Set logs
	pt.SetLogs("comp/function/app", "npm ERR! code ENOENT\nnpm ERR! missing script: dev")
	pt.SetError("comp/function/app", assert.AnError)
	pt.PrintFinalSummary()

	output := buf.String()
	assert.Contains(t, output, "Output:")
	assert.Contains(t, output, "npm ERR! code ENOENT")
	assert.Contains(t, output, "npm ERR! missing script: dev")
}

func TestProgressTable_PrintFinalSummary_TruncatesLongLogs(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	pt.AddResource("comp/function/app", "app", "function", "comp", nil)

	// Generate 50 lines of logs (should be truncated to last 30)
	var logs strings.Builder
	for i := 1; i <= 50; i++ {
		logs.WriteString("Log line " + string(rune('0'+i/10)) + string(rune('0'+i%10)) + "\n")
	}
	pt.SetLogs("comp/function/app", logs.String())
	pt.SetError("comp/function/app", assert.AnError)
	pt.PrintFinalSummary()

	output := buf.String()
	assert.Contains(t, output, "lines truncated")
	// Should contain the later lines but not the early ones
	assert.Contains(t, output, "Log line")
}

func TestStatusIcon(t *testing.T) {
	buf := &bytes.Buffer{}
	pt := NewProgressTable(buf)

	tests := []struct {
		status ResourceStatus
		want   string
	}{
		{StatusPending, "○"},
		{StatusWaiting, "◔"},
		{StatusInProgress, "◐"},
		{StatusCompleted, "●"},
		{StatusFailed, "✗"},
		{StatusSkipped, "◌"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := pt.statusIcon(tt.status)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatResourceType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"database", "Database"},
		{"DEPLOYMENT", "Deployment"},
		{"function", "Function"},
		{"", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := formatResourceType(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResourceID(t *testing.T) {
	id := resourceID("mycomp", "database", "main")
	assert.Equal(t, "mycomp/database/main", id)
}
