package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// ResourceStatus represents the current status of a resource.
type ResourceStatus string

const (
	StatusPending    ResourceStatus = "pending"
	StatusWaiting    ResourceStatus = "waiting"
	StatusInProgress ResourceStatus = "in_progress"
	StatusCompleted  ResourceStatus = "completed"
	StatusFailed     ResourceStatus = "failed"
	StatusSkipped    ResourceStatus = "skipped"
)

// ResourceInfo holds information about a resource for progress tracking.
type ResourceInfo struct {
	Name         string
	Type         string
	Component    string
	Status       ResourceStatus
	Dependencies []string
	StartTime    time.Time
	EndTime      time.Time
	Error        error
	Message      string
	// InferredConfig stores detected configuration for debugging
	InferredConfig map[string]string
	// Logs stores captured output for debugging failures
	Logs string
}

// ProgressTable displays deployment progress.
// It shows an initial plan table, then tracks status silently for the final summary.
type ProgressTable struct {
	mu        sync.Mutex
	resources map[string]*ResourceInfo
	order     []string // Maintains insertion order for display
	writer    io.Writer
	startTime time.Time
}

// NewProgressTable creates a new progress table.
func NewProgressTable(w io.Writer) *ProgressTable {
	return &ProgressTable{
		resources: make(map[string]*ResourceInfo),
		order:     []string{},
		writer:    w,
		startTime: time.Now(),
	}
}

// AddResource adds a resource to track.
func (p *ProgressTable) AddResource(id, name, resourceType, component string, dependencies []string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.resources[id]; !exists {
		p.order = append(p.order, id)
	}

	status := StatusPending
	if len(dependencies) > 0 {
		status = StatusWaiting
	}

	p.resources[id] = &ResourceInfo{
		Name:         name,
		Type:         resourceType,
		Component:    component,
		Status:       status,
		Dependencies: dependencies,
	}
}

// UpdateStatus updates the status of a resource (tracked silently for final summary).
func (p *ProgressTable) UpdateStatus(id string, status ResourceStatus, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	res, ok := p.resources[id]
	if !ok {
		return
	}

	res.Status = status
	res.Message = message

	if status == StatusInProgress && res.StartTime.IsZero() {
		res.StartTime = time.Now()
	}
	if status == StatusCompleted || status == StatusFailed || status == StatusSkipped {
		res.EndTime = time.Now()
	}
}

// SetError sets an error for a resource.
func (p *ProgressTable) SetError(id string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	res, ok := p.resources[id]
	if !ok {
		return
	}

	res.Status = StatusFailed
	res.Error = err
	res.EndTime = time.Now()
}

// SetInferredConfig stores the inferred configuration for a resource.
func (p *ProgressTable) SetInferredConfig(id string, config map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if res, ok := p.resources[id]; ok {
		res.InferredConfig = config
	}
}

// SetLogs stores captured logs for a resource.
func (p *ProgressTable) SetLogs(id string, logs string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if res, ok := p.resources[id]; ok {
		res.Logs = logs
	}
}

// AppendLogs appends to the captured logs for a resource.
func (p *ProgressTable) AppendLogs(id string, logs string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if res, ok := p.resources[id]; ok {
		res.Logs += logs
	}
}


// PrintInitial prints the deployment plan showing what resources will be created.
func (p *ProgressTable) PrintInitial() {
	p.mu.Lock()
	defer p.mu.Unlock()

	fmt.Fprintln(p.writer)
	fmt.Fprintln(p.writer, "Deployment Plan:")
	fmt.Fprintln(p.writer, strings.Repeat("─", 60))

	// Group resources by type for cleaner display
	byType := make(map[string][]*ResourceInfo)
	for _, id := range p.order {
		res := p.resources[id]
		byType[res.Type] = append(byType[res.Type], res)
	}

	// Print in a logical order
	typeOrder := []string{"database", "bucket", "build", "function", "deployment", "service", "route"}
	for _, resType := range typeOrder {
		resources := byType[resType]
		if len(resources) == 0 {
			continue
		}
		for _, res := range resources {
			deps := ""
			if len(res.Dependencies) > 0 {
				depNames := p.getDependencyNames(res.Dependencies)
				deps = fmt.Sprintf(" (depends on: %s)", strings.Join(depNames, ", "))
			}
			fmt.Fprintf(p.writer, "  + %-12s %s%s\n", res.Type, res.Name, deps)
		}
	}

	// Print any types not in the standard order
	for resType, resources := range byType {
		found := false
		for _, t := range typeOrder {
			if t == resType {
				found = true
				break
			}
		}
		if !found {
			for _, res := range resources {
				deps := ""
				if len(res.Dependencies) > 0 {
					depNames := p.getDependencyNames(res.Dependencies)
					deps = fmt.Sprintf(" (depends on: %s)", strings.Join(depNames, ", "))
				}
				fmt.Fprintf(p.writer, "  + %-12s %s%s\n", res.Type, res.Name, deps)
			}
		}
	}

	fmt.Fprintln(p.writer, strings.Repeat("─", 60))
	fmt.Fprintf(p.writer, "Total: %d resources\n", len(p.order))
	fmt.Fprintln(p.writer)
}

// PrintUpdate prints a status update for a resource as a single line.
func (p *ProgressTable) PrintUpdate(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	res, ok := p.resources[id]
	if !ok {
		return
	}

	var statusStr string
	switch res.Status {
	case StatusInProgress:
		statusStr = fmt.Sprintf("%s Starting %s/%s...", p.statusIcon(res.Status), res.Type, res.Name)
	case StatusCompleted:
		duration := ""
		if !res.EndTime.IsZero() && !res.StartTime.IsZero() {
			duration = fmt.Sprintf(" (%s)", res.EndTime.Sub(res.StartTime).Round(time.Millisecond))
		}
		statusStr = fmt.Sprintf("%s %s/%s completed%s", p.statusIcon(res.Status), res.Type, res.Name, duration)
	case StatusFailed:
		statusStr = fmt.Sprintf("%s %s/%s failed", p.statusIcon(res.Status), res.Type, res.Name)
		if res.Error != nil {
			statusStr += fmt.Sprintf(": %v", res.Error)
		}
	case StatusSkipped:
		statusStr = fmt.Sprintf("%s %s/%s skipped", p.statusIcon(res.Status), res.Type, res.Name)
	default:
		return // Don't print pending/waiting updates
	}

	fmt.Fprintln(p.writer, statusStr)
}

func (p *ProgressTable) statusIcon(status ResourceStatus) string {
	switch status {
	case StatusPending:
		return "○"
	case StatusWaiting:
		return "◔"
	case StatusInProgress:
		return "◐"
	case StatusCompleted:
		return "●"
	case StatusFailed:
		return "✗"
	case StatusSkipped:
		return "◌"
	default:
		return "?"
	}
}

func (p *ProgressTable) getDependencyNames(depIDs []string) []string {
	names := make([]string, 0, len(depIDs))
	for _, depID := range depIDs {
		if res, ok := p.resources[depID]; ok {
			names = append(names, res.Name)
		} else {
			// Extract name from ID (format: component/type/name)
			parts := strings.Split(depID, "/")
			if len(parts) >= 3 {
				names = append(names, parts[len(parts)-1])
			} else {
				names = append(names, depID)
			}
		}
	}
	return names
}

// PrintFinalSummary prints the final deployment summary.
func (p *ProgressTable) PrintFinalSummary() {
	p.mu.Lock()
	defer p.mu.Unlock()

	var completed, failed, skipped int
	for _, res := range p.resources {
		switch res.Status {
		case StatusCompleted:
			completed++
		case StatusFailed:
			failed++
		case StatusSkipped:
			skipped++
		}
	}

	elapsed := time.Since(p.startTime).Round(time.Millisecond)

	fmt.Fprintln(p.writer)
	fmt.Fprintln(p.writer, strings.Repeat("─", 80))

	if failed > 0 {
		fmt.Fprintf(p.writer, "Deployment completed with errors in %s\n", elapsed)
		fmt.Fprintf(p.writer, "  ● %d succeeded, ✗ %d failed, ◌ %d skipped\n", completed, failed, skipped)

		// List failed resources with detailed information
		fmt.Fprintln(p.writer, "\nFailed resources:")
		for _, id := range p.order {
			res := p.resources[id]
			if res.Status == StatusFailed {
				fmt.Fprintf(p.writer, "\n  ✗ %s %q", res.Type, res.Name)
				if res.Error != nil {
					fmt.Fprintf(p.writer, ": %v", res.Error)
				}
				fmt.Fprintln(p.writer)

				// Show inferred configuration if available
				if len(res.InferredConfig) > 0 {
					fmt.Fprintln(p.writer, "\n    Inferred configuration:")
					// Sort keys for consistent output
					keys := make([]string, 0, len(res.InferredConfig))
					for k := range res.InferredConfig {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, k := range keys {
						v := res.InferredConfig[k]
						if v != "" {
							fmt.Fprintf(p.writer, "      %s: %s\n", k, v)
						}
					}
				}

				// Show logs if available
				if res.Logs != "" {
					fmt.Fprintln(p.writer, "\n    Output:")
					lines := strings.Split(strings.TrimSpace(res.Logs), "\n")
					// Limit to last 30 lines to avoid overwhelming output
					startIdx := 0
					if len(lines) > 30 {
						startIdx = len(lines) - 30
						fmt.Fprintf(p.writer, "      ... (%d lines truncated)\n", startIdx)
					}
					for _, line := range lines[startIdx:] {
						fmt.Fprintf(p.writer, "      %s\n", line)
					}
				}
			}
		}
	} else {
		fmt.Fprintf(p.writer, "Deployment completed successfully in %s\n", elapsed)
		fmt.Fprintf(p.writer, "  ● %d resources deployed\n", completed)
	}
}

// GetCompletedCount returns the number of completed resources.
func (p *ProgressTable) GetCompletedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	count := 0
	for _, res := range p.resources {
		if res.Status == StatusCompleted {
			count++
		}
	}
	return count
}

// GetFailedCount returns the number of failed resources.
func (p *ProgressTable) GetFailedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	count := 0
	for _, res := range p.resources {
		if res.Status == StatusFailed {
			count++
		}
	}
	return count
}

// HasPending returns true if there are pending or waiting resources.
func (p *ProgressTable) HasPending() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, res := range p.resources {
		if res.Status == StatusPending || res.Status == StatusWaiting || res.Status == StatusInProgress {
			return true
		}
	}
	return false
}

// GetResourcesByStatus returns all resource IDs with the given status.
func (p *ProgressTable) GetResourcesByStatus(status ResourceStatus) []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	var ids []string
	for _, id := range p.order {
		if p.resources[id].Status == status {
			ids = append(ids, id)
		}
	}
	return ids
}

// CheckDependencies updates waiting resources to pending if their dependencies are met.
func (p *ProgressTable) CheckDependencies() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	var ready []string

	for _, id := range p.order {
		res := p.resources[id]
		if res.Status != StatusWaiting {
			continue
		}

		allMet := true
		for _, depID := range res.Dependencies {
			if dep, ok := p.resources[depID]; ok {
				if dep.Status != StatusCompleted {
					allMet = false
					break
				}
			}
		}

		if allMet {
			res.Status = StatusPending
			ready = append(ready, id)
		}
	}

	return ready
}

// formatResourceType returns a formatted resource type string.
func formatResourceType(t string) string {
	// Capitalize first letter and format nicely
	if t == "" {
		return "Unknown"
	}
	return strings.ToUpper(t[:1]) + strings.ToLower(t[1:])
}

// SortedResourceIDs returns resource IDs sorted by dependency order.
func (p *ProgressTable) SortedResourceIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Topological sort
	visited := make(map[string]bool)
	var result []string

	var visit func(id string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true

		res := p.resources[id]
		for _, depID := range res.Dependencies {
			visit(depID)
		}
		result = append(result, id)
	}

	// Sort order for determinism
	ids := make([]string, len(p.order))
	copy(ids, p.order)
	sort.Strings(ids)

	for _, id := range ids {
		visit(id)
	}

	return result
}
