# output

Live output streaming for cldctl operations. Supports event-driven output with multiple handlers, progress tracking, and formatted console/JSON output.

## Overview

The `output` package provides:

- Event-driven architecture with pluggable handlers
- Multiple output formats (console with colors, JSON)
- Progress tracking with progress bars
- Thread-safe event processing
- Line-by-line scanning from readers

## Types

### Level

Log levels for events.

```go
const (
    LevelDebug Level = iota
    LevelInfo
    LevelWarn
    LevelError
)
```

### Event

Streaming event structure.

```go
type Event struct {
    Time      time.Time
    Level     Level
    Component string
    Resource  string
    Action    string
    Message   string
    Progress  int
    Metadata  map[string]interface{}
}
```

### Handler

Interface for processing events.

```go
type Handler interface {
    HandleEvent(event Event)
    Close() error
}
```

## Stream

The main stream manages event distribution to handlers.

### Creating a Stream

```go
import "github.com/davidthor/cldctl/pkg/output"

stream := output.NewStream()
```

### Adding Handlers

```go
// Console handler with colors
stream.AddHandler(output.NewConsoleHandler(output.ConsoleOptions{
    Writer:    os.Stdout,
    UseColors: true,
    Verbose:   false,
}))

// JSON handler for structured output
stream.AddHandler(output.NewJSONHandler(os.Stdout))
```

### Emitting Events

```go
// Emit a full event
stream.Emit(output.Event{
    Level:     output.LevelInfo,
    Component: "api",
    Resource:  "deployment",
    Action:    "create",
    Message:   "Creating deployment...",
})

// Convenience methods
stream.EmitInfo("api", "deployment", "Creating deployment...")
stream.EmitProgress("api", "deployment", "create", 50)  // 50% progress
stream.EmitError("api", "deployment", err)
```

### Writer Interface

Get an `io.Writer` that converts writes to events:

```go
// Create a writer for a specific component/resource
writer := stream.Writer("api", "build", output.LevelInfo)

// Use with exec.Command or other writers
cmd := exec.Command("docker", "build", ".")
cmd.Stdout = writer
cmd.Stderr = stream.Writer("api", "build", output.LevelError)
cmd.Run()
```

### Closing the Stream

```go
// Close the stream and all handlers
err := stream.Close()
```

## Handlers

### ConsoleHandler

Writes events to console with optional color formatting.

```go
handler := output.NewConsoleHandler(output.ConsoleOptions{
    Writer:    os.Stdout,  // Output destination
    UseColors: true,       // Enable ANSI colors
    Verbose:   false,      // Show debug messages
})

stream.AddHandler(handler)
```

### JSONHandler

Writes events as JSON lines (JSONL format).

```go
handler := output.NewJSONHandler(os.Stdout)
stream.AddHandler(handler)
```

Output format:

```json
{"time":"2024-01-15T10:30:00Z","level":"info","component":"api","resource":"deployment","message":"Creating..."}
{"time":"2024-01-15T10:30:01Z","level":"info","component":"api","resource":"deployment","message":"Created"}
```

## Progress Tracking

### ProgressBar

Text-based progress bar for tracking operations.

```go
// Create a progress bar (100 total steps)
bar := output.NewProgressBar(stream, "api", "build", 100)

// Update progress
bar.Increment()     // +1
bar.Add(10)         // +10
bar.SetCurrent(50)  // Set to 50%

// Mark complete
bar.Complete()
```

## Line Scanner

Scans an `io.Reader` and emits events for each line.

```go
scanner := output.NewLineScanner(stream, "api", "build", output.LevelInfo)

// Scan from a reader (blocks until EOF or context cancellation)
err := scanner.Scan(ctx, reader)
```

Useful for streaming command output:

```go
cmd := exec.Command("docker", "build", ".")
stdout, _ := cmd.StdoutPipe()

go scanner.Scan(ctx, stdout)
cmd.Run()
```

## Utility Functions

### MultiWriter

Creates a writer that duplicates writes to multiple writers.

```go
multi := output.MultiWriter(os.Stdout, logFile)
// Writes go to both stdout and the log file
```

## Example: Full Usage

```go
import (
    "context"
    "os"
    "os/exec"
    "github.com/davidthor/cldctl/pkg/output"
)

func main() {
    // Create stream with handlers
    stream := output.NewStream()
    stream.AddHandler(output.NewConsoleHandler(output.ConsoleOptions{
        Writer:    os.Stdout,
        UseColors: true,
    }))
    defer stream.Close()

    // Emit status updates
    stream.EmitInfo("api", "build", "Starting build...")

    // Create progress bar
    bar := output.NewProgressBar(stream, "api", "build", 100)

    // Run a command with output streaming
    cmd := exec.Command("docker", "build", "-t", "myapp", ".")
    cmd.Stdout = stream.Writer("api", "build", output.LevelInfo)
    cmd.Stderr = stream.Writer("api", "build", output.LevelError)

    if err := cmd.Run(); err != nil {
        stream.EmitError("api", "build", err)
        return
    }

    bar.Complete()
    stream.EmitInfo("api", "build", "Build complete!")
}
```

## Thread Safety

- The `Stream` type is thread-safe for concurrent event emission
- Uses channels for async event processing with a buffer of 100 events
- Handlers are protected by mutexes
- Graceful shutdown with event draining

## Architecture

```
┌─────────────────┐
│    Emitters     │  (EmitInfo, EmitProgress, Writer, etc.)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│     Stream      │  (event channel, handler management)
└────────┬────────┘
         │
    ┌────┴────┐
    ▼         ▼
┌───────┐ ┌───────┐
│Console│ │ JSON  │  (handlers)
│Handler│ │Handler│
└───────┘ └───────┘
```
