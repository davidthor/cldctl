package cli

import (
	"github.com/architect-io/arcctl/pkg/engine"
	"github.com/architect-io/arcctl/pkg/iac"
	"github.com/architect-io/arcctl/pkg/state"

	// Import IaC plugins to trigger registration via init() functions
	_ "github.com/architect-io/arcctl/pkg/iac/container"
	_ "github.com/architect-io/arcctl/pkg/iac/native"
	_ "github.com/architect-io/arcctl/pkg/iac/opentofu"
	_ "github.com/architect-io/arcctl/pkg/iac/pulumi"
)

// createEngine creates a new deployment engine with the given state manager.
// The IaC plugins are automatically registered via init() functions from the
// blank imports above.
func createEngine(stateManager state.Manager) *engine.Engine {
	return engine.NewEngine(stateManager, iac.DefaultRegistry)
}

// defaultParallelism is the default number of parallel operations for deployments.
const defaultParallelism = 10
