package logs

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// ANSI color codes for workload label prefixes.
var colors = []string{
	"\033[36m", // cyan
	"\033[33m", // yellow
	"\033[32m", // green
	"\033[35m", // magenta
	"\033[34m", // blue
	"\033[31m", // red
	"\033[96m", // bright cyan
	"\033[93m", // bright yellow
	"\033[92m", // bright green
	"\033[95m", // bright magenta
}

const colorReset = "\033[0m"

// MultiplexOptions configures how multiplexed log output is formatted.
type MultiplexOptions struct {
	// ShowTimestamps prefixes each line with the entry's timestamp.
	ShowTimestamps bool

	// NoColor disables ANSI color codes in the output.
	NoColor bool
}

// FormatQueryResult writes a QueryResult to the writer with color-coded
// workload prefixes. Entries are sorted by timestamp before writing.
func FormatQueryResult(w io.Writer, result *QueryResult, opts MultiplexOptions) {
	if len(result.Entries) == 0 {
		return
	}

	// Sort entries by timestamp for deterministic interleaving.
	sorted := make([]LogEntry, len(result.Entries))
	copy(sorted, result.Entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	// Compute the label for each entry and find the longest label for alignment.
	labels := make([]string, len(sorted))
	maxLen := 0
	for i, entry := range sorted {
		labels[i] = entryLabel(entry)
		if len(labels[i]) > maxLen {
			maxLen = len(labels[i])
		}
	}

	colorMap := buildColorMap(sorted, opts.NoColor)

	for i, entry := range sorted {
		writeEntry(w, entry, labels[i], maxLen, colorMap, opts)
	}
}

// FormatStream reads from a LogStream and writes formatted entries to the
// writer until the stream ends or the context in the stream closes.
// This function blocks until the stream is exhausted or an error occurs.
func FormatStream(w io.Writer, stream *LogStream, opts MultiplexOptions) error {
	colorMap := map[string]string{}
	colorIdx := 0
	maxLen := 0
	var mu sync.Mutex

	for {
		select {
		case entry, ok := <-stream.Entries:
			if !ok {
				return nil // stream closed normally
			}

			label := entryLabel(entry)

			mu.Lock()
			// Assign color if new label
			if !opts.NoColor {
				if _, exists := colorMap[label]; !exists {
					colorMap[label] = colors[colorIdx%len(colors)]
					colorIdx++
				}
			}
			if len(label) > maxLen {
				maxLen = len(label)
			}
			mu.Unlock()

			writeEntry(w, entry, label, maxLen, colorMap, opts)

		case err := <-stream.Err:
			return err
		}
	}
}

// entryLabel builds a "component/type/name" label from a log entry's labels.
func entryLabel(entry LogEntry) string {
	ns := entry.Labels["service_namespace"]
	svcType := entry.Labels["service_type"]
	name := entry.Labels["service_name"]

	if ns == "" && name == "" {
		return "unknown"
	}

	// service_name is typically "<component>-<workload>". If service_namespace
	// is present, strip the component prefix for a cleaner label.
	workload := name
	if ns != "" && name != "" {
		workload = strings.TrimPrefix(name, ns+"-")
	}

	// Build the label with available information
	var parts []string
	if ns != "" {
		parts = append(parts, ns)
	}
	if svcType != "" {
		parts = append(parts, svcType)
	}
	if workload != "" {
		parts = append(parts, workload)
	}

	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, "/")
}

// buildColorMap assigns a unique color to each label found in the entries.
func buildColorMap(entries []LogEntry, noColor bool) map[string]string {
	if noColor {
		return map[string]string{}
	}

	seen := map[string]bool{}
	var orderedLabels []string
	for _, entry := range entries {
		label := entryLabel(entry)
		if !seen[label] {
			seen[label] = true
			orderedLabels = append(orderedLabels, label)
		}
	}

	colorMap := make(map[string]string, len(orderedLabels))
	for i, label := range orderedLabels {
		colorMap[label] = colors[i%len(colors)]
	}
	return colorMap
}

// writeEntry formats and writes a single log entry to the writer.
func writeEntry(w io.Writer, entry LogEntry, label string, maxLen int, colorMap map[string]string, opts MultiplexOptions) {
	var sb strings.Builder

	// Color prefix
	color := colorMap[label]
	if color != "" {
		sb.WriteString(color)
	}

	// Padded label
	sb.WriteString(fmt.Sprintf("%-*s", maxLen, label))
	sb.WriteString(" | ")

	if color != "" {
		sb.WriteString(colorReset)
	}

	// Optional timestamp
	if opts.ShowTimestamps {
		sb.WriteString(entry.Timestamp.Format("2006-01-02T15:04:05.000Z07:00"))
		sb.WriteString("  ")
	}

	// Log line
	sb.WriteString(entry.Line)
	sb.WriteString("\n")

	fmt.Fprint(w, sb.String())
}
