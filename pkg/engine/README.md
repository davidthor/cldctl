# engine

Core orchestration engine for cldctl deployments. Coordinates component deployments, manages state, and orchestrates the execution pipeline.

## Overview

The `engine` package is the heart of cldctl's deployment system. It provides:

- Deployment orchestration
- Dependency graph construction and traversal
- Execution planning (diff between desired and current state)
- Parallel and sequential execution
- Expression parsing and evaluation

## Package Structure

```
engine/
├── engine.go      # Main Engine type and deployment orchestration
├── executor/      # Execution of planned changes
├── expression/    # Expression parsing and evaluation (${{ }})
└── planner/       # Execution plan generation

# The graph package is at pkg/graph/ for broader reusability
../graph/          # Dependency graph construction and traversal (pkg/graph)
```

## Main Engine

### Types

```go
type Engine struct {
    // ...
}

type DeployOptions struct {
    Environment  string
    Datacenter   string
    Components   []string
    Variables    map[string]string
    Output       *output.Stream
    DryRun       bool
    AutoApprove  bool
    Parallelism  int
}

type DeployResult struct {
    Success   bool
    Plan      *planner.Plan
    Execution *executor.ExecutionResult
    Duration  time.Duration
}

type DestroyOptions struct {
    Environment string
    Output      *output.Stream
    DryRun      bool
    AutoApprove bool
}

type DestroyResult struct {
    Success   bool
    Plan      *planner.Plan
    Execution *executor.ExecutionResult
    Duration  time.Duration
}
```

### Functions

```go
// Create a new deployment engine
engine := engine.NewEngine(stateManager, iacRegistry)

// Deploy components to an environment
result, err := engine.Deploy(ctx, engine.DeployOptions{
    Environment: "production",
    Datacenter:  "aws-us-east",
    Components:  []string{"./api", "./web"},
    Variables:   map[string]string{"domain": "example.com"},
    Parallelism: 4,
})

// Deploy from an environment.yml file
result, err := engine.DeployFromEnvironmentFile(ctx, "./environments/prod.yml", opts)

// Destroy an environment
result, err := engine.Destroy(ctx, engine.DestroyOptions{
    Environment: "staging",
})
```

## Subpackages

### executor

Executes planned changes sequentially or in parallel, respecting dependencies.

```go
import "github.com/davidthor/cldctl/pkg/engine/executor"

// Create an executor with options
exec := executor.NewExecutor(stateManager, iacRegistry, executor.Options{
    Parallelism: 4,
    Output:      outputStream,
    DryRun:      false,
    StopOnError: true,
})

// Execute a plan sequentially
result, err := exec.Execute(ctx, plan, graph)

// Execute independent operations in parallel
result, err := exec.ExecuteParallel(ctx, plan, graph)
```

**Types:**

- `Executor` - Executes planned changes
- `ExecutionResult` - Results of an execution (Success, Duration, Created, Updated, Deleted, Failed, Errors)
- `NodeResult` - Result of executing a single node
- `Options` - Executor configuration

### expression

Parses and evaluates `${{ }}` expressions against a context.

```go
import "github.com/davidthor/cldctl/pkg/engine/expression"

// Parse an expression
parser := expression.NewParser()
expr, err := parser.Parse("Hello, ${{ variables.name | upper }}!")

// Evaluate the expression
evaluator := expression.NewEvaluator()
ctx := expression.NewEvalContext()
ctx.Variables = map[string]string{"name": "world"}

result, err := evaluator.EvaluateString(expr, ctx)
// result: "Hello, WORLD!"

// Check if a string contains expressions
hasExpr := expression.ContainsExpression("${{ foo }}")  // true
```

**Built-in Pipe Functions:**

- `join` - Joins array elements with a separator
- `first` - Returns the first element of an array
- `last` - Returns the last element of an array
- `length` - Returns the length of an array or string
- `default` - Returns the value or a default if nil/empty
- `upper` - Converts a string to uppercase
- `lower` - Converts a string to lowercase
- `trim` - Trims whitespace from a string

**Context Types:**

- `EvalContext` - Values for expression evaluation
- `DatabaseOutputs` - Outputs from a provisioned database
- `BucketOutputs` - Outputs from a provisioned bucket
- `ServiceOutputs` - Outputs from a provisioned service
- `RouteOutputs` - Outputs from a provisioned route
- `FunctionOutputs` - Outputs from a provisioned function
- `DependencyOutputs` - Outputs from a dependency component
- `DependentOutputs` - Outputs from a dependent component

### graph (pkg/graph)

Builds dependency graphs from component specifications and provides topological sorting.
This package is located at `pkg/graph/` (not under engine) for broader reusability,
such as rendering topology without executing.

```go
import "github.com/davidthor/cldctl/pkg/graph"

// Create a new graph
g := graph.NewGraph("production", "aws")

// Create and add nodes
node := graph.NewNode(graph.NodeTypeDeployment, "api", "main")
g.AddNode(node)

// Add dependency edges
g.AddEdge("api/main", "database/postgres")

// Get nodes in deployment order (dependencies first)
nodes, err := g.TopologicalSort()

// Get nodes in destruction order (dependents first)
nodes, err := g.ReverseTopologicalSort()

// Get all nodes ready to execute
readyNodes := g.GetReadyNodes()

// Check completion status
if g.AllCompleted() {
    // All nodes finished
}
if g.HasFailed() {
    // At least one node failed
}
```

**Node Types:**

- `NodeTypeDatabase`
- `NodeTypeBucket`
- `NodeTypeDeployment`
- `NodeTypeFunction`
- `NodeTypeService`
- `NodeTypeRoute`
- `NodeTypeCronjob`
- `NodeTypeSecret`
- `NodeTypeDockerBuild`
- `NodeTypeMigration`

**Node States:**

- `NodeStatePending`
- `NodeStateRunning`
- `NodeStateCompleted`
- `NodeStateFailed`
- `NodeStateSkipped`

**Builder:**

```go
builder := graph.NewBuilder("production", "aws")
builder.AddComponent("my-app", component)  // Component name provided externally
g := builder.Build()
```

### planner

Generates execution plans by comparing desired state with current state.

```go
import "github.com/davidthor/cldctl/pkg/engine/planner"

// Create a planner
p := planner.NewPlanner()

// Create an execution plan
plan, err := p.Plan(graph, currentState)

// Create a destruction plan
plan, err := p.PlanDestroy(graph, currentState)

// Check if there are changes
if plan.IsEmpty() {
    fmt.Println("No changes needed")
}

// Access planned changes
for _, change := range plan.Changes {
    fmt.Printf("%s: %s (%s)\n", change.Action, change.Node.ID, change.Reason)
}
```

**Actions:**

- `ActionCreate` - Resource will be created
- `ActionUpdate` - Resource will be updated in place
- `ActionReplace` - Resource will be destroyed and recreated
- `ActionDelete` - Resource will be destroyed
- `ActionNoop` - No changes needed

**Types:**

- `Planner` - Generates execution plans
- `Plan` - Execution plan (Changes, ToCreate, ToUpdate, ToDelete, NoChange)
- `ResourceChange` - Planned change to a resource
- `PropertyChange` - Change to a property

## Architecture Flow

```
1. Engine: Entry point for deployment operations
         ↓
2. Graph Builder: Constructs dependency graphs from component specifications
         ↓
3. Planner: Generates execution plans by comparing desired vs current state
         ↓
4. Executor: Executes plans sequentially or in parallel
         ↓
5. Expression: Evaluates dynamic expressions during graph construction and execution
```

## Example: Full Deployment Flow

```go
import (
    "github.com/davidthor/cldctl/pkg/engine"
    "github.com/davidthor/cldctl/pkg/iac"
    "github.com/davidthor/cldctl/pkg/state"
)

// Initialize dependencies
stateManager, _ := state.NewManagerFromConfig(stateConfig)
iacRegistry := iac.DefaultRegistry

// Create the engine
eng := engine.NewEngine(stateManager, iacRegistry)

// Deploy
result, err := eng.Deploy(ctx, engine.DeployOptions{
    Environment: "production",
    Datacenter:  "aws-us-east",
    Components:  []string{"./components/api"},
    Variables: map[string]string{
        "replicas": "3",
        "domain":   "api.example.com",
    },
    Parallelism: 4,
    AutoApprove: false,
})

if err != nil {
    log.Fatal(err)
}

fmt.Printf("Deployment %s in %v\n",
    map[bool]string{true: "succeeded", false: "failed"}[result.Success],
    result.Duration)
```
