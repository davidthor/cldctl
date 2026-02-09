package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
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

// ANSI color codes for dynamic table rendering.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorDim    = "\033[90m"
	ansiErase   = "\033[2K" // erase entire line
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
//
// When the output writer is a terminal, the table renders dynamically —
// resource status lines are redrawn in place using ANSI escape codes so the
// user sees a live-updating table. When the writer is not a terminal (e.g.,
// piped to a file or CI logs), each status change is printed as a single
// append-only line for readability.
type ProgressTable struct {
	mu        sync.Mutex
	resources map[string]*ResourceInfo
	order     []string // Maintains insertion order for display
	writer    io.Writer
	startTime time.Time

	// dynamic is true when the writer is a terminal that supports ANSI codes.
	dynamic bool
	// tableLines tracks how many lines the dynamic table occupies so that
	// subsequent redraws can move the cursor back to overwrite them.
	tableLines int
}

// NewProgressTable creates a new progress table.
// If the writer is a terminal, the table will render dynamically.
func NewProgressTable(w io.Writer) *ProgressTable {
	dynamic := false
	if f, ok := w.(*os.File); ok {
		dynamic = term.IsTerminal(int(f.Fd()))
	}

	return &ProgressTable{
		resources: make(map[string]*ResourceInfo),
		order:     []string{},
		writer:    w,
		startTime: time.Now(),
		dynamic:   dynamic,
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

// UpdateStatus updates the status of a resource.
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

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// PrintInitial prints the initial deployment state.
func (p *ProgressTable) PrintInitial() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.dynamic {
		// Dynamic mode: compact header + live table
		fmt.Fprintf(p.writer, "\nDeploying %d resources...\n\n", len(p.order))
		p.renderTableLocked()
		return
	}

	// Non-dynamic mode: verbose plan listing (backward-compatible)
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

// PrintUpdate displays progress for the given resource.
func (p *ProgressTable) PrintUpdate(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.dynamic {
		p.renderTableLocked()
		return
	}

	// Non-dynamic: single-line append-only updates
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

// PrintFinalSummary prints the final deployment summary.
func (p *ProgressTable) PrintFinalSummary() {
	p.mu.Lock()
	defer p.mu.Unlock()

	var completed, rootFailed, cascaded, skipped int
	for _, id := range p.order {
		res := p.resources[id]
		switch res.Status {
		case StatusCompleted:
			completed++
		case StatusFailed:
			if p.isCascadedFailure(res) {
				cascaded++
			} else {
				rootFailed++
			}
		case StatusSkipped:
			skipped++
		}
	}

	anyFailed := rootFailed > 0 || cascaded > 0
	elapsed := time.Since(p.startTime).Round(time.Millisecond)

	if p.dynamic {
		// Render one final table update so the summary line shows the final counts.
		p.renderTableLocked()

		// On success the dynamic table's summary line is sufficient — no extra
		// output needed.
		if !anyFailed {
			fmt.Fprintln(p.writer)
			return
		}
	}

	// Print separator
	fmt.Fprintln(p.writer)
	fmt.Fprintln(p.writer, strings.Repeat("─", 80))

	if anyFailed {
		fmt.Fprintf(p.writer, "Deployment FAILED (%s)\n", elapsed)
		parts := []string{fmt.Sprintf("● %d succeeded", completed)}
		if rootFailed > 0 {
			parts = append(parts, fmt.Sprintf("✗ %d failed", rootFailed))
		}
		if cascaded > 0 {
			parts = append(parts, fmt.Sprintf("○ %d cancelled", cascaded))
		}
		if skipped > 0 {
			parts = append(parts, fmt.Sprintf("◌ %d skipped", skipped))
		}
		fmt.Fprintf(p.writer, "  %s\n", strings.Join(parts, "  "))

		// Separate root-cause failures from cascaded/stopped failures
		var rootFailures, cascadedFailures []*ResourceInfo
		for _, id := range p.order {
			res := p.resources[id]
			if res.Status != StatusFailed {
				continue
			}
			if p.isCascadedFailure(res) {
				cascadedFailures = append(cascadedFailures, res)
			} else {
				rootFailures = append(rootFailures, res)
			}
		}

		// Show root-cause failures first with full detail
		if len(rootFailures) > 0 {
			fmt.Fprintln(p.writer, "\nErrors:")
			for _, res := range rootFailures {
				fmt.Fprintf(p.writer, "  ✗ %s/%s", res.Type, res.Name)
				if res.Error != nil {
					fmt.Fprintf(p.writer, ": %v", res.Error)
				}
				fmt.Fprintln(p.writer)

				// Show inferred configuration if available
				if len(res.InferredConfig) > 0 {
					fmt.Fprintln(p.writer, "    Inferred configuration:")
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
					fmt.Fprintln(p.writer, "    Output:")
					lines := strings.Split(strings.TrimSpace(res.Logs), "\n")
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

		// Show cascaded failures as a compact list
		if len(cascadedFailures) > 0 {
			fmt.Fprintf(p.writer, "\nSkipped due to above errors: %d resources\n", len(cascadedFailures))
		}

		fmt.Fprintln(p.writer)
	} else {
		fmt.Fprintf(p.writer, "Deployment completed successfully in %s\n", elapsed)
		fmt.Fprintf(p.writer, "  ● %d resources deployed\n", completed)
	}
}

// ---------------------------------------------------------------------------
// Dynamic table renderer (ANSI terminal)
// ---------------------------------------------------------------------------

// renderTableLocked draws (or redraws) the live progress table.
// Caller MUST hold p.mu.
func (p *ProgressTable) renderTableLocked() {
	// Move cursor up to overwrite the previous render.
	if p.tableLines > 0 {
		fmt.Fprintf(p.writer, "\033[%dA", p.tableLines)
	}

	lines := 0

	// ---- compute column widths ----
	maxLabelLen := 0
	components := make(map[string]bool)
	for _, id := range p.order {
		res := p.resources[id]
		label := res.Type + "/" + res.Name
		if len(label) > maxLabelLen {
			maxLabelLen = len(label)
		}
		components[res.Component] = true
	}
	multiComp := len(components) > 1

	maxCompLen := 0
	if multiComp {
		for comp := range components {
			if len(comp) > maxCompLen {
				maxCompLen = len(comp)
			}
		}
	}

	// ---- render each resource row ----
	for _, id := range p.order {
		res := p.resources[id]
		icon := p.coloredIconForResource(res)
		label := res.Type + "/" + res.Name
		desc := p.statusDescription(res)
		deps := p.dependencyColumn(res)

		if multiComp {
			compName := colorDim + fmt.Sprintf("%-*s", maxCompLen, res.Component) + colorReset
			fmt.Fprintf(p.writer, "%s  %s  %s  %-*s  %s%s\n",
				ansiErase, icon, compName, maxLabelLen, label, desc, deps)
		} else {
			fmt.Fprintf(p.writer, "%s  %s  %-*s  %s%s\n",
				ansiErase, icon, maxLabelLen, label, desc, deps)
		}
		lines++
	}

	// ---- summary / progress line ----
	var completed, rootFailed, cascaded int
	allDone := true
	for _, id := range p.order {
		res := p.resources[id]
		switch res.Status {
		case StatusCompleted:
			completed++
		case StatusFailed:
			if p.isCascadedFailure(res) {
				cascaded++
			} else {
				rootFailed++
			}
		case StatusPending, StatusWaiting, StatusInProgress:
			allDone = false
		}
	}
	total := len(p.order)
	elapsed := time.Since(p.startTime).Round(time.Second)

	// blank separator line
	fmt.Fprintf(p.writer, "%s\n", ansiErase)
	lines++

	if allDone {
		if rootFailed > 0 || cascaded > 0 {
			extra := ""
			if cascaded > 0 {
				extra = fmt.Sprintf(", %d cancelled", cascaded)
			}
			fmt.Fprintf(p.writer, "%s  %s✗ %d/%d completed, %d failed%s%s (%s)\n",
				ansiErase, colorRed, completed, total, rootFailed, extra, colorReset, elapsed)
		} else {
			fmt.Fprintf(p.writer, "%s  %s● %d/%d deployed%s (%s)\n",
				ansiErase, colorGreen, completed, total, colorReset, elapsed)
		}
	} else {
		fmt.Fprintf(p.writer, "%s  %d/%d completed (%s)\n",
			ansiErase, completed, total, elapsed)
	}
	lines++

	p.tableLines = lines
}

// dependencyColumn returns a formatted dependency hint for a resource row.
// Once all dependencies are completed, it returns an empty string so the
// column disappears. For long dependency lists it shows a count instead.
//
// For cascaded/cancelled failures, it shows the root-cause failure(s) that
// triggered the cancellation instead of graph dependencies — even if the
// resource has no direct dependency on the failed resource.
func (p *ProgressTable) dependencyColumn(res *ResourceInfo) string {
	// For cascaded failures, show what caused the cancellation.
	if p.isCascadedFailure(res) {
		return p.rootCauseColumn(res)
	}

	if len(res.Dependencies) == 0 {
		return ""
	}

	// Collect only incomplete dependencies.
	var pending []string
	for _, depID := range res.Dependencies {
		if dep, ok := p.resources[depID]; ok {
			if dep.Status != StatusCompleted {
				pending = append(pending, dep.Type+"/"+dep.Name)
			}
		}
	}

	if len(pending) == 0 {
		// All dependencies satisfied — hide the column.
		return ""
	}

	short := strings.Join(pending, ", ")
	const maxLen = 30
	if len(short) <= maxLen {
		return "  " + colorDim + "← " + short + colorReset
	}
	// Too long — show count instead.
	return fmt.Sprintf("  %s← %d deps%s", colorDim, len(pending), colorReset)
}

// rootCauseColumn returns a formatted hint showing which resource(s) caused a
// cascaded failure. It only shows a cause when the error message names a
// specific dependency (i.e., the resource has a real graph dependency on the
// failed resource). Resources cancelled by StopOnError context cancellation
// show no cause — attributing a specific failure to them would be misleading.
func (p *ProgressTable) rootCauseColumn(res *ResourceInfo) string {
	if res.Error == nil {
		return ""
	}
	errMsg := res.Error.Error()

	// "dependency <id> failed" — extract the specific dependency.
	if strings.HasPrefix(errMsg, "dependency ") && strings.HasSuffix(errMsg, " failed") {
		depID := strings.TrimPrefix(errMsg, "dependency ")
		depID = strings.TrimSuffix(depID, " failed")
		if dep, ok := p.resources[depID]; ok {
			return "  " + colorDim + "← " + dep.Type + "/" + dep.Name + colorReset
		}
	}

	// "dependencies failed: X, Y" — extract multiple.
	if strings.HasPrefix(errMsg, "dependencies failed: ") {
		depIDsStr := strings.TrimPrefix(errMsg, "dependencies failed: ")
		depIDs := strings.Split(depIDsStr, ", ")
		var names []string
		for _, depID := range depIDs {
			if dep, ok := p.resources[depID]; ok {
				names = append(names, dep.Type+"/"+dep.Name)
			}
		}
		if len(names) > 0 {
			return p.formatCauseList(names)
		}
	}

	// "deployment stopped" / "cancelled" — no specific cause to attribute.
	return ""
}

// formatCauseList formats a list of root-cause resource names for the
// dependency/cause column, truncating to a count when the text is too long.
func (p *ProgressTable) formatCauseList(names []string) string {
	short := strings.Join(names, ", ")
	const maxLen = 30
	if len(short) <= maxLen {
		return "  " + colorDim + "← " + short + colorReset
	}
	return fmt.Sprintf("  %s← %d failures%s", colorDim, len(names), colorReset)
}

// coloredIcon returns the status icon wrapped in an ANSI color code.
// Cascaded failures (dependency/cancelled) use a dim open circle instead of
// the red ✗ so the user can focus on the real root-cause errors.
func (p *ProgressTable) coloredIcon(status ResourceStatus) string {
	switch status {
	case StatusPending:
		return colorDim + "○" + colorReset
	case StatusWaiting:
		return colorDim + "◔" + colorReset
	case StatusInProgress:
		return colorYellow + "◐" + colorReset
	case StatusCompleted:
		return colorGreen + "●" + colorReset
	case StatusFailed:
		return colorRed + "✗" + colorReset
	case StatusSkipped:
		return colorDim + "◌" + colorReset
	default:
		return "?"
	}
}

// coloredIconForResource is like coloredIcon but considers whether the failure
// is a cascaded/cancelled one (shown dimmed) vs a real root-cause error.
func (p *ProgressTable) coloredIconForResource(res *ResourceInfo) string {
	if res.Status == StatusFailed && p.isCascadedFailure(res) {
		return colorDim + "○" + colorReset
	}
	return p.coloredIcon(res.Status)
}

// statusDescription returns a human-readable description for the resource's
// current status, with ANSI color codes.
func (p *ProgressTable) statusDescription(res *ResourceInfo) string {
	switch res.Status {
	case StatusPending:
		return colorDim + "pending" + colorReset
	case StatusWaiting:
		return colorDim + "waiting" + colorReset
	case StatusInProgress:
		verb := "deploying"
		switch res.Type {
		case "build":
			verb = "building"
		case "database", "bucket", "service", "route", "network", "volume":
			verb = "creating"
		}
		return colorYellow + verb + "..." + colorReset
	case StatusCompleted:
		duration := ""
		if !res.EndTime.IsZero() && !res.StartTime.IsZero() {
			duration = fmt.Sprintf(" (%s)", res.EndTime.Sub(res.StartTime).Round(time.Millisecond))
		}
		return colorGreen + "done" + duration + colorReset
	case StatusFailed:
		if p.isCascadedFailure(res) {
			return colorDim + "cancelled" + colorReset
		}
		msg := "FAILED"
		if res.Error != nil {
			errStr := res.Error.Error()
			// Keep error messages short for the table row
			if len(errStr) > 60 {
				errStr = errStr[:57] + "..."
			}
			msg += ": " + errStr
		}
		return colorRed + msg + colorReset
	case StatusSkipped:
		return colorDim + "skipped" + colorReset
	default:
		return ""
	}
}

// isCascadedFailure returns true if the resource failed because of a dependency
// failure, a deployment stop, or context cancellation — not because of an error
// in the resource itself. These are visually distinguished from root-cause errors.
func (p *ProgressTable) isCascadedFailure(res *ResourceInfo) bool {
	if res.Status != StatusFailed {
		return false
	}
	if res.Error == nil {
		return false
	}
	errMsg := res.Error.Error()
	return strings.HasPrefix(errMsg, "dependency ") ||
		strings.HasPrefix(errMsg, "deployment stopped:") ||
		errMsg == "cancelled"
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

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
			names = append(names, res.Type+"/"+res.Name)
		} else {
			// Extract type/name from ID (format: component/type/name)
			parts := strings.Split(depID, "/")
			if len(parts) >= 3 {
				names = append(names, parts[len(parts)-2]+"/"+parts[len(parts)-1])
			} else {
				names = append(names, depID)
			}
		}
	}
	return names
}

// ---------------------------------------------------------------------------
// Query helpers (unchanged)
// ---------------------------------------------------------------------------

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
