package graph

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/davidthor/cldctl/pkg/schema/component"
	"github.com/davidthor/cldctl/pkg/schema/datacenter"
	dcv1 "github.com/davidthor/cldctl/pkg/schema/datacenter/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadDatacenter is a test helper that parses an HCL datacenter specification.
func loadDatacenter(t *testing.T, hclStr string) datacenter.Datacenter {
	t.Helper()
	loader := datacenter.NewLoader()
	dc, err := loader.LoadFromBytes([]byte(hclStr), "test.dc")
	require.NoError(t, err, "failed to load datacenter")
	return dc
}

// makeTestHookFilter replicates the engine.makeHookFilter() logic for use in
// tests. It creates an ImplicitNodeFilter that evaluates each hook's when-clause
// against the prospective node's inputs using the v1 HCL evaluator.
func makeTestHookFilter(hooks []datacenter.Hook) ImplicitNodeFilter {
	return func(inputs map[string]interface{}) bool {
		for _, hook := range hooks {
			when := hook.When()
			if when == "" {
				return true // catch-all
			}
			expr, diags := hclsyntax.ParseExpression([]byte(when), "when.hcl", hcl.Pos{Line: 1, Column: 1})
			if diags.HasErrors() {
				return true // conservative: can't parse → assume match
			}
			eval := dcv1.NewEvaluator()
			eval.SetNodeContext("", "", "", inputs)
			if result, err := eval.EvaluateWhen(expr); err == nil && result {
				return true
			}
		}
		return false
	}
}

// buildGraphFromDatacenter replicates the engine.Deploy() pattern:
// 1. Load datacenter to detect hooks
// 2. Configure implicit node filters based on hook when-clauses
// 3. Add components to graph
// This is the integration point we want to verify.
func buildGraphFromDatacenter(t *testing.T, dc datacenter.Datacenter, components map[string]component.Component) *Graph {
	t.Helper()
	builder := NewBuilder("test-env", "test-dc")

	// Replicate engine.go hook filter logic — evaluate when-clauses to decide
	// which implicit nodes to create (mirrors makeHookFilter in engine.go).
	if env := dc.Environment(); env != nil {
		if hooks := env.Hooks(); hooks != nil {
			if dbUserHooks := hooks.DatabaseUser(); len(dbUserHooks) > 0 {
				builder.SetDatabaseUserFilter(makeTestHookFilter(dbUserHooks))
			}
			if npHooks := hooks.NetworkPolicy(); len(npHooks) > 0 {
				builder.SetNetworkPolicyFilter(makeTestHookFilter(npHooks))
			}
		}
	}

	for name, comp := range components {
		err := builder.AddComponent(name, comp)
		require.NoError(t, err, "failed to add component %s", name)
	}

	return builder.Build()
}

// ---------------------------------------------------------------------------
// Test 1: Datacenter hook detection drives EnableImplicitNodes correctly
// ---------------------------------------------------------------------------

func TestIntegration_HookDetection_BothHooksPresent(t *testing.T) {
	dc := loadDatacenter(t, `
environment {
  databaseUser {
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = { name = "test" }
    }
    outputs = {
      url = "postgres://localhost/test"
    }
  }

  networkPolicy {
    module "net_policy" {
      plugin = "native"
      build  = "./modules/net-policy"
      inputs = { from = "test" }
    }
  }
}
`)

	env := dc.Environment()
	require.NotNil(t, env)
	hooks := env.Hooks()
	require.NotNil(t, hooks)

	assert.Greater(t, len(hooks.DatabaseUser()), 0, "expected databaseUser hooks")
	assert.Greater(t, len(hooks.NetworkPolicy()), 0, "expected networkPolicy hooks")

	// When both hooks are present, the graph should create both implicit node types
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      AUTH_URL: "${{ services.auth.url }}"

services:
  auth:
    deployment: api
    port: 8080
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	assert.NotNil(t, g.GetNode("app/databaseUser/main--api"), "expected databaseUser node when hook is defined")
	assert.NotNil(t, g.GetNode("app/networkPolicy/api--auth"), "expected networkPolicy node when hook is defined")
}

func TestIntegration_HookDetection_OnlyDatabaseUserHook(t *testing.T) {
	dc := loadDatacenter(t, `
environment {
  databaseUser {
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = { name = "test" }
    }
    outputs = {
      url = "postgres://localhost/test"
    }
  }
}
`)

	hooks := dc.Environment().Hooks()
	assert.Greater(t, len(hooks.DatabaseUser()), 0)
	assert.Equal(t, 0, len(hooks.NetworkPolicy()))

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      AUTH_URL: "${{ services.auth.url }}"

services:
  auth:
    deployment: api
    port: 8080
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	assert.NotNil(t, g.GetNode("app/databaseUser/main--api"), "expected databaseUser node")
	assert.Nil(t, g.GetNode("app/networkPolicy/api--auth"), "should not create networkPolicy without hook")
}

func TestIntegration_HookDetection_OnlyNetworkPolicyHook(t *testing.T) {
	dc := loadDatacenter(t, `
environment {
  networkPolicy {
    module "net_policy" {
      plugin = "native"
      build  = "./modules/net-policy"
      inputs = { from = "test" }
    }
  }
}
`)

	hooks := dc.Environment().Hooks()
	assert.Equal(t, 0, len(hooks.DatabaseUser()))
	assert.Greater(t, len(hooks.NetworkPolicy()), 0)

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      AUTH_URL: "${{ services.auth.url }}"

services:
  auth:
    deployment: api
    port: 8080
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	assert.Nil(t, g.GetNode("app/databaseUser/main--api"), "should not create databaseUser without hook")
	assert.NotNil(t, g.GetNode("app/networkPolicy/api--auth"), "expected networkPolicy node")
}

func TestIntegration_HookDetection_NoImplicitHooks(t *testing.T) {
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = "localhost"
      port = "5432"
      url  = "postgres://localhost/test"
    }
  }
}
`)

	hooks := dc.Environment().Hooks()
	assert.Equal(t, 0, len(hooks.DatabaseUser()))
	assert.Equal(t, 0, len(hooks.NetworkPolicy()))

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      AUTH_URL: "${{ services.auth.url }}"

services:
  auth:
    deployment: api
    port: 8080
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	// Neither implicit node type should be created
	assert.Nil(t, g.GetNode("app/databaseUser/main--api"))
	assert.Nil(t, g.GetNode("app/networkPolicy/api--auth"))

	// But the core nodes should exist
	assert.NotNil(t, g.GetNode("app/database/main"))
	assert.NotNil(t, g.GetNode("app/deployment/api"))
	assert.NotNil(t, g.GetNode("app/service/auth"))

	// The deployment should depend directly on the database (no databaseUser in between)
	apiNode := g.GetNode("app/deployment/api")
	assert.Contains(t, apiNode.DependsOn, "app/database/main",
		"deployment should depend directly on database when no databaseUser hook defined")
}

// ---------------------------------------------------------------------------
// Test 2: Full graph construction from realistic component + datacenter
// ---------------------------------------------------------------------------

func TestIntegration_FullGraph_WebAppWithDatabase(t *testing.T) {
	// A datacenter with database and deployment hooks (typical production setup)
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = {
        name = "${environment.name}-${node.component}-${node.name}"
        type = node.inputs.type
      }
    }
    outputs = {
      host = module.db.host
      port = module.db.port
      url  = module.db.url
    }
  }

  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = {
        name  = "${environment.name}-${node.component}-${node.name}"
        image = node.inputs.image
      }
    }
    outputs = {
      id = module.deploy.id
    }
  }

  service {
    module "svc" {
      plugin = "native"
      build  = "./modules/svc"
      inputs = {
        name = node.name
        port = node.inputs.port
      }
    }
    outputs = {
      host = module.svc.host
      port = module.svc.port
      url  = module.svc.url
    }
  }

  route {
    module "route" {
      plugin = "native"
      build  = "./modules/route"
      inputs = {
        name = node.name
      }
    }
    outputs = {
      url  = module.route.url
      host = module.route.host
      port = module.route.port
    }
  }
}
`)

	// A typical web app: database, deployment, service, route
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: my-app:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      PORT: "8080"

services:
  api:
    deployment: api
    port: 8080

routes:
  main:
    type: http
    service: api
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"my-app": comp})

	// Verify all expected nodes exist
	dbNode := g.GetNode("my-app/database/main")
	deployNode := g.GetNode("my-app/deployment/api")
	svcNode := g.GetNode("my-app/service/api")
	routeNode := g.GetNode("my-app/route/main")

	require.NotNil(t, dbNode, "expected database node")
	require.NotNil(t, deployNode, "expected deployment node")
	require.NotNil(t, svcNode, "expected service node")
	require.NotNil(t, routeNode, "expected route node")

	// Verify node types
	assert.Equal(t, NodeTypeDatabase, dbNode.Type)
	assert.Equal(t, NodeTypeDeployment, deployNode.Type)
	assert.Equal(t, NodeTypeService, svcNode.Type)
	assert.Equal(t, NodeTypeRoute, routeNode.Type)

	// Deployment should depend on database (via environment variable expression)
	assert.Contains(t, deployNode.DependsOn, dbNode.ID, "deployment should depend on database")

	// Routes and services intentionally do NOT depend on their targets.
	// Services are stable networking abstractions (like k8s Services);
	// Routes are external routing config. Both can exist before backends.
	assert.Empty(t, routeNode.DependsOn, "route should have no dependencies (target is metadata only)")
	assert.Empty(t, svcNode.DependsOn, "service should have no dependencies (target is metadata only)")

	// Verify no implicit nodes (no databaseUser or networkPolicy hooks)
	assert.Nil(t, g.GetNode("my-app/databaseUser/main--api"),
		"should not create databaseUser without hook")

	// Topological sort should succeed
	sorted, err := g.TopologicalSort()
	require.NoError(t, err)
	assert.Len(t, sorted, 4, "expected 4 nodes")

	// Database must come before deployment (which references it via env var)
	nodeIndex := make(map[string]int)
	for i, n := range sorted {
		nodeIndex[n.ID] = i
	}
	assert.Less(t, nodeIndex[dbNode.ID], nodeIndex[deployNode.ID])
}

func TestIntegration_FullGraph_WithDatabaseUserHook(t *testing.T) {
	// A datacenter that defines a databaseUser hook (like the local datacenter)
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = {
        name = "${environment.name}-${node.component}-${node.name}"
      }
    }
    outputs = {
      host = module.db.host
      port = module.db.port
      url  = module.db.url
    }
  }

  databaseUser {
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = {
        name     = "${environment.name}-${node.component}-${node.name}"
        database = node.inputs.database
      }
    }
    outputs = {
      url = module.db_user.url
    }
  }

  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = {
        name = node.name
      }
    }
    outputs = {
      id = module.deploy.id
    }
  }
}
`)

	// Component with two workloads referencing the same database
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
  worker:
    image: worker:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	// Verify core nodes
	dbNode := g.GetNode("app/database/main")
	apiNode := g.GetNode("app/deployment/api")
	workerNode := g.GetNode("app/deployment/worker")
	require.NotNil(t, dbNode)
	require.NotNil(t, apiNode)
	require.NotNil(t, workerNode)

	// Verify databaseUser nodes were created (one per consumer)
	dbUserAPI := g.GetNode("app/databaseUser/main--api")
	dbUserWorker := g.GetNode("app/databaseUser/main--worker")
	require.NotNil(t, dbUserAPI, "expected databaseUser node for api consumer")
	require.NotNil(t, dbUserWorker, "expected databaseUser node for worker consumer")

	// Verify dependency chain: api → databaseUser/main--api → database
	assert.Contains(t, dbUserAPI.DependsOn, dbNode.ID,
		"databaseUser should depend on database")
	assert.Contains(t, apiNode.DependsOn, dbUserAPI.ID,
		"api deployment should depend on its databaseUser, not the database directly")
	assert.NotContains(t, apiNode.DependsOn, dbNode.ID,
		"api should NOT depend directly on database when databaseUser is interposed")

	// Same for worker
	assert.Contains(t, dbUserWorker.DependsOn, dbNode.ID)
	assert.Contains(t, workerNode.DependsOn, dbUserWorker.ID)
	assert.NotContains(t, workerNode.DependsOn, dbNode.ID)

	// Verify databaseUser inputs carry database metadata
	assert.Equal(t, "main", dbUserAPI.Inputs["database"])
	assert.Equal(t, "api", dbUserAPI.Inputs["consumer"])
	assert.Equal(t, "worker", dbUserWorker.Inputs["consumer"])

	// Topological sort should succeed
	sorted, err := g.TopologicalSort()
	require.NoError(t, err)

	nodeIndex := make(map[string]int)
	for i, n := range sorted {
		nodeIndex[n.ID] = i
	}

	// database < databaseUser < deployment
	assert.Less(t, nodeIndex[dbNode.ID], nodeIndex[dbUserAPI.ID])
	assert.Less(t, nodeIndex[dbUserAPI.ID], nodeIndex[apiNode.ID])
	assert.Less(t, nodeIndex[dbNode.ID], nodeIndex[dbUserWorker.ID])
	assert.Less(t, nodeIndex[dbUserWorker.ID], nodeIndex[workerNode.ID])
}

func TestIntegration_FullGraph_DatabaseUserHookWithWhenClause(t *testing.T) {
	// Mirrors the local datacenter pattern: databaseUser hook only matches postgres.
	// Redis databases should NOT get databaseUser nodes because the hook's
	// when clause is evaluated at graph construction time and filters out
	// non-matching database types. The deployment connects directly to redis.
	dc := loadDatacenter(t, `
environment {
  database {
    when = element(split(":", node.inputs.type), 0) == "postgres"
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = module.db.host
      port = module.db.port
      url  = module.db.url
    }
  }

  database {
    when = element(split(":", node.inputs.type), 0) == "redis"
    module "redis" {
      plugin = "native"
      build  = "./modules/redis"
      inputs = { name = "test" }
    }
    outputs = {
      host = module.redis.host
      port = module.redis.port
      url  = module.redis.url
    }
  }

  databaseUser {
    when = element(split(":", node.inputs.type), 0) == "postgres"
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = { name = "test" }
    }
    outputs = {
      url = module.db_user.url
    }
  }

  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = {
      id = module.deploy.id
    }
  }
}
`)

	// Component with both postgres and redis, both referenced by the same deployment
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
  cache:
    type: redis:^7

deployments:
  api:
    image: api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      REDIS_URL: "${{ databases.cache.url }}"
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	// Postgres: databaseUser hook matches → databaseUser node created
	dbUserPostgres := g.GetNode("app/databaseUser/main--api")
	require.NotNil(t, dbUserPostgres, "expected databaseUser node for postgres database")
	assert.Equal(t, "postgres:^16", dbUserPostgres.Inputs["type"])

	// Redis: databaseUser hook does NOT match → no databaseUser node, deployment
	// depends directly on the database node instead.
	dbUserRedis := g.GetNode("app/databaseUser/cache--api")
	assert.Nil(t, dbUserRedis, "redis should NOT get a databaseUser node — no matching hook")

	// Deployment should depend on the postgres databaseUser AND directly on redis database
	apiNode := g.GetNode("app/deployment/api")
	require.NotNil(t, apiNode)
	assert.Contains(t, apiNode.DependsOn, dbUserPostgres.ID, "api should depend on postgres databaseUser")
	assert.Contains(t, apiNode.DependsOn, "app/database/cache", "api should depend directly on redis database")
	assert.NotContains(t, apiNode.DependsOn, "app/database/main", "api should NOT depend directly on postgres database (goes through databaseUser)")

	// Topological sort should succeed
	sorted, err := g.TopologicalSort()
	require.NoError(t, err)
	// 2 databases + 1 databaseUser (postgres only) + 1 deployment = 4
	assert.Len(t, sorted, 4)
}

func TestIntegration_FullGraph_WithMigrations(t *testing.T) {
	// Datacenter with databaseUser hook
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = module.db.host
      port = module.db.port
      url  = module.db.url
    }
  }

  databaseUser {
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = { name = "test" }
    }
    outputs = {
      url = module.db_user.url
    }
  }

  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = {
      id = module.deploy.id
    }
  }

  task {
    module "task" {
      plugin = "native"
      build  = "./modules/task"
      inputs = { name = "test" }
    }
    outputs = {
      id     = module.task.id
      status = module.task.status
    }
  }
}
`)

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
    migrations:
      image: my-app-migrations:latest
      command: ["npm", "run", "migrate"]

deployments:
  api:
    image: api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	// Verify migration task node
	taskNode := g.GetNode("app/task/main-migration")
	require.NotNil(t, taskNode, "expected migration task node")
	assert.Equal(t, NodeTypeTask, taskNode.Type)

	// Task depends on database
	dbNode := g.GetNode("app/database/main")
	require.NotNil(t, dbNode)
	assert.Contains(t, taskNode.DependsOn, dbNode.ID)

	// Deployment should depend on the databaseUser AND the migration task
	apiNode := g.GetNode("app/deployment/api")
	require.NotNil(t, apiNode)

	dbUserNode := g.GetNode("app/databaseUser/main--api")
	require.NotNil(t, dbUserNode, "expected databaseUser node")

	assert.Contains(t, apiNode.DependsOn, dbUserNode.ID,
		"deployment should depend on databaseUser")
	assert.Contains(t, apiNode.DependsOn, taskNode.ID,
		"deployment should depend on migration task")

	// Topological sort should succeed — no cycles
	sorted, err := g.TopologicalSort()
	require.NoError(t, err)

	nodeIndex := make(map[string]int)
	for i, n := range sorted {
		nodeIndex[n.ID] = i
	}

	// database < task AND database < databaseUser < deployment
	assert.Less(t, nodeIndex[dbNode.ID], nodeIndex[taskNode.ID])
	assert.Less(t, nodeIndex[dbNode.ID], nodeIndex[dbUserNode.ID])
	assert.Less(t, nodeIndex[taskNode.ID], nodeIndex[apiNode.ID])
	assert.Less(t, nodeIndex[dbUserNode.ID], nodeIndex[apiNode.ID])
}

func TestIntegration_FullGraph_ComplexComponent(t *testing.T) {
	// Datacenter with all hook types (simulates a real production DC)
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = module.db.host
      port = module.db.port
      url  = module.db.url
    }
  }

  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = {
      id = module.deploy.id
    }
  }

  function {
    module "fn" {
      plugin = "native"
      build  = "./modules/fn"
      inputs = { name = "test" }
    }
    outputs = {
      id       = module.fn.id
      endpoint = module.fn.endpoint
    }
  }

  service {
    module "svc" {
      plugin = "native"
      build  = "./modules/svc"
      inputs = { name = "test" }
    }
    outputs = {
      host = module.svc.host
      port = module.svc.port
      url  = module.svc.url
    }
  }

  route {
    module "route" {
      plugin = "native"
      build  = "./modules/route"
      inputs = { name = "test" }
    }
    outputs = {
      url  = module.route.url
      host = module.route.host
      port = module.route.port
    }
  }

  bucket {
    module "bucket" {
      plugin = "native"
      build  = "./modules/bucket"
      inputs = { name = "test" }
    }
    outputs = {
      endpoint       = module.bucket.endpoint
      bucket         = module.bucket.bucket
      accessKeyId    = module.bucket.access_key
      secretAccessKey = module.bucket.secret_key
    }
  }
}
`)

	// A complex component with databases, buckets, deployments, functions,
	// services, and routes — exercises the full graph construction pipeline.
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
  cache:
    type: redis:^7

buckets:
  uploads:
    type: s3

deployments:
  api:
    image: api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      REDIS_URL: "${{ databases.cache.url }}"
      S3_ENDPOINT: "${{ buckets.uploads.endpoint }}"

functions:
  web:
    src:
      path: .
      framework: nextjs
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      API_URL: "${{ services.api.url }}"

services:
  api:
    deployment: api
    port: 8080

routes:
  main:
    type: http
    service: api
  api:
    type: http
    function: web
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"my-app": comp})

	// Verify all nodes
	expectedNodes := map[string]NodeType{
		"my-app/database/main":    NodeTypeDatabase,
		"my-app/database/cache":   NodeTypeDatabase,
		"my-app/bucket/uploads":   NodeTypeBucket,
		"my-app/deployment/api":   NodeTypeDeployment,
		"my-app/function/web":     NodeTypeFunction,
		"my-app/service/api":      NodeTypeService,
		"my-app/route/main":       NodeTypeRoute,
		"my-app/route/api":        NodeTypeRoute,
	}

	for nodeID, expectedType := range expectedNodes {
		node := g.GetNode(nodeID)
		require.NotNil(t, node, "expected node %s to exist", nodeID)
		assert.Equal(t, expectedType, node.Type, "wrong type for %s", nodeID)
	}

	// Count total nodes (no implicit nodes since no databaseUser/networkPolicy hooks)
	assert.Equal(t, len(expectedNodes), len(g.Nodes),
		"expected exactly %d nodes (no implicit nodes without hooks)", len(expectedNodes))

	// Verify key dependencies
	apiDeploy := g.GetNode("my-app/deployment/api")
	assert.Contains(t, apiDeploy.DependsOn, "my-app/database/main")
	assert.Contains(t, apiDeploy.DependsOn, "my-app/database/cache")
	assert.Contains(t, apiDeploy.DependsOn, "my-app/bucket/uploads")

	webFn := g.GetNode("my-app/function/web")
	assert.Contains(t, webFn.DependsOn, "my-app/database/main")
	assert.Contains(t, webFn.DependsOn, "my-app/service/api")

	apiSvc := g.GetNode("my-app/service/api")
	// Services don't depend on their target deployments (stable networking abstraction)
	assert.Empty(t, apiSvc.DependsOn)

	// Topological sort should succeed
	sorted, err := g.TopologicalSort()
	require.NoError(t, err)
	assert.Len(t, sorted, len(expectedNodes))
}

// ---------------------------------------------------------------------------
// Test 3: Multi-component graph with inter-component dependencies
// ---------------------------------------------------------------------------

func TestIntegration_MultiComponent_IndependentComponents(t *testing.T) {
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = module.db.host
      port = module.db.port
      url  = module.db.url
    }
  }

  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = {
      id = module.deploy.id
    }
  }
}
`)

	compA := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: app-a:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	compB := loadComponent(t, `
databases:
  main:
    type: redis:^7

deployments:
  worker:
    image: app-b:latest
    environment:
      REDIS_URL: "${{ databases.main.url }}"
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{
		"app-a": compA,
		"app-b": compB,
	})

	// Both components should have their own namespaced nodes
	assert.NotNil(t, g.GetNode("app-a/database/main"))
	assert.NotNil(t, g.GetNode("app-a/deployment/api"))
	assert.NotNil(t, g.GetNode("app-b/database/main"))
	assert.NotNil(t, g.GetNode("app-b/deployment/worker"))

	// Nodes from different components should NOT depend on each other
	apiNode := g.GetNode("app-a/deployment/api")
	assert.NotContains(t, apiNode.DependsOn, "app-b/database/main",
		"component A should not depend on component B's resources")

	workerNode := g.GetNode("app-b/deployment/worker")
	assert.NotContains(t, workerNode.DependsOn, "app-a/database/main",
		"component B should not depend on component A's resources")

	// Topological sort should succeed (no cross-component cycles)
	sorted, err := g.TopologicalSort()
	require.NoError(t, err)
	assert.Len(t, sorted, 4)
}

func TestIntegration_MultiComponent_WithDependencies(t *testing.T) {
	dc := loadDatacenter(t, `
environment {
  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = {
      id = module.deploy.id
    }
  }
}
`)

	// Component A depends on Component B
	compA := loadComponent(t, `
dependencies:
  auth-service: ghcr.io/myorg/auth:v1

deployments:
  api:
    image: api:latest
    environment:
      AUTH_URL: "${{ dependencies.auth-service.url }}"
`)

	compB := loadComponent(t, `
deployments:
  server:
    image: auth:latest
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{
		"my-app":       compA,
		"auth-service": compB,
	})

	// Verify both components' nodes exist
	assert.NotNil(t, g.GetNode("my-app/deployment/api"))
	assert.NotNil(t, g.GetNode("auth-service/deployment/server"))

	// Verify component-level dependency is tracked
	require.NotNil(t, g.ComponentDependencies)
	assert.Contains(t, g.ComponentDependencies["my-app"], "auth-service",
		"my-app should declare auth-service as a dependency")

	// The auth-service should NOT have dependencies
	assert.Empty(t, g.ComponentDependencies["auth-service"])
}

func TestIntegration_MultiComponent_OptionalDependency(t *testing.T) {
	dc := loadDatacenter(t, `
environment {
  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = {
      id = module.deploy.id
    }
  }
}
`)

	comp := loadComponent(t, `
dependencies:
  analytics:
    source: ghcr.io/myorg/analytics:v1
    optional: true
  auth:
    source: ghcr.io/myorg/auth:v1

deployments:
  api:
    image: api:latest
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"my-app": comp})

	// Required dependency should be in ComponentDependencies
	require.NotNil(t, g.ComponentDependencies)
	assert.Contains(t, g.ComponentDependencies["my-app"], "auth",
		"required dependency should be tracked")

	// Optional dependency should NOT be in ComponentDependencies
	assert.NotContains(t, g.ComponentDependencies["my-app"], "analytics",
		"optional dependency should not be in ComponentDependencies")

	// Optional dependency should be in OptionalDependencies
	require.NotNil(t, g.OptionalDependencies)
	require.NotNil(t, g.OptionalDependencies["my-app"])
	assert.True(t, g.OptionalDependencies["my-app"]["analytics"],
		"analytics should be marked as optional")
}

// ---------------------------------------------------------------------------
// Test 4: Datacenter validation regression tests
// ---------------------------------------------------------------------------

func TestIntegration_DatacenterValidation_DatabaseUserHookMinimalOutputs(t *testing.T) {
	// Regression test: databaseUser hooks should only require "url" as output.
	// Previously required "host", "port", "url" which broke all official templates.
	dc := loadDatacenter(t, `
environment {
  databaseUser {
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = { name = "test" }
    }
    outputs = {
      url = "postgres://user:pass@localhost/db"
    }
  }
}
`)

	hooks := dc.Environment().Hooks()
	assert.Len(t, hooks.DatabaseUser(), 1)
}

func TestIntegration_DatacenterValidation_DatabaseUserHookWithAllOutputs(t *testing.T) {
	// A databaseUser hook that provides all outputs (like the updated local DC)
	dc := loadDatacenter(t, `
environment {
  databaseUser {
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = { name = "test" }
    }
    outputs = {
      host     = "localhost"
      port     = "5432"
      username = "app_user"
      password = "secret"
      url      = "postgres://app_user:secret@localhost/db"
    }
  }
}
`)

	hooks := dc.Environment().Hooks()
	assert.Len(t, hooks.DatabaseUser(), 1)

	// Verify the hook's outputs are accessible
	hook := hooks.DatabaseUser()[0]
	outputs := hook.Outputs()
	assert.Equal(t, "localhost", outputs["host"])
	assert.Equal(t, "5432", outputs["port"])
	assert.Equal(t, "postgres://app_user:secret@localhost/db", outputs["url"])
}

func TestIntegration_DatacenterValidation_DatabaseUserHookMissingURL(t *testing.T) {
	// A databaseUser hook missing the required "url" output should fail validation.
	loader := datacenter.NewLoader()
	_, err := loader.LoadFromBytes([]byte(`
environment {
  databaseUser {
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = { name = "test" }
    }
    outputs = {
      username = "app_user"
      password = "secret"
    }
  }
}
`), "test.dc")

	require.Error(t, err, "databaseUser hook missing 'url' should fail validation")
	assert.Contains(t, err.Error(), "url", "error should mention the missing 'url' output")
}

func TestIntegration_DatacenterValidation_EmptyEnvironment(t *testing.T) {
	// A datacenter with an empty environment block should load fine
	dc := loadDatacenter(t, `environment {}`)

	env := dc.Environment()
	require.NotNil(t, env)
	hooks := env.Hooks()
	require.NotNil(t, hooks)

	assert.Empty(t, hooks.Database())
	assert.Empty(t, hooks.DatabaseUser())
	assert.Empty(t, hooks.NetworkPolicy())
	assert.Empty(t, hooks.Deployment())
}

// ---------------------------------------------------------------------------
// Test 5: Graph node counts match expectations (regression guard)
// ---------------------------------------------------------------------------

func TestIntegration_NodeCount_NoImplicitNodes(t *testing.T) {
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = "localhost"
      port = "5432"
      url  = "postgres://localhost/test"
    }
  }

  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = {
      id = module.deploy.id
    }
  }

  service {
    module "svc" {
      plugin = "native"
      build  = "./modules/svc"
      inputs = { name = "test" }
    }
    outputs = {
      host = module.svc.host
      port = module.svc.port
      url  = module.svc.url
    }
  }
}
`)

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
  cache:
    type: redis:^7

deployments:
  api:
    image: api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      REDIS_URL: "${{ databases.cache.url }}"
      AUTH_URL: "${{ services.auth.url }}"
  worker:
    image: worker:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"

services:
  auth:
    deployment: api
    port: 8080
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	// Expected: 2 databases + 2 deployments + 1 service = 5 nodes
	// No databaseUser or networkPolicy nodes (no hooks defined)
	assert.Equal(t, 5, len(g.Nodes), "expected exactly 5 nodes without implicit hooks")
}

func TestIntegration_NodeCount_WithDatabaseUserHook(t *testing.T) {
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = "localhost"
      port = "5432"
      url  = "postgres://localhost/test"
    }
  }

  databaseUser {
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = { name = "test" }
    }
    outputs = {
      url = "postgres://user:pass@localhost/test"
    }
  }

  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = {
      id = module.deploy.id
    }
  }

  service {
    module "svc" {
      plugin = "native"
      build  = "./modules/svc"
      inputs = { name = "test" }
    }
    outputs = {
      host = module.svc.host
      port = module.svc.port
      url  = module.svc.url
    }
  }
}
`)

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
  cache:
    type: redis:^7

deployments:
  api:
    image: api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      REDIS_URL: "${{ databases.cache.url }}"
      AUTH_URL: "${{ services.auth.url }}"
  worker:
    image: worker:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"

services:
  auth:
    deployment: api
    port: 8080
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	// Expected: 2 databases + 2 deployments + 1 service = 5 core nodes
	// + databaseUser nodes: main--api, cache--api, main--worker = 3 databaseUser nodes
	// = 8 total nodes
	assert.Equal(t, 8, len(g.Nodes),
		"expected 8 nodes: 5 core + 3 databaseUser (no networkPolicy hook)")

	// Verify each databaseUser node exists
	assert.NotNil(t, g.GetNode("app/databaseUser/main--api"))
	assert.NotNil(t, g.GetNode("app/databaseUser/cache--api"))
	assert.NotNil(t, g.GetNode("app/databaseUser/main--worker"))
}

func TestIntegration_NodeCount_WithBothImplicitHooks(t *testing.T) {
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = "localhost"
      port = "5432"
      url  = "postgres://localhost/test"
    }
  }

  databaseUser {
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = { name = "test" }
    }
    outputs = {
      url = "postgres://user:pass@localhost/test"
    }
  }

  networkPolicy {
    module "net_policy" {
      plugin = "native"
      build  = "./modules/net-policy"
      inputs = { from = "test" }
    }
  }

  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = {
      id = module.deploy.id
    }
  }

  service {
    module "svc" {
      plugin = "native"
      build  = "./modules/svc"
      inputs = { name = "test" }
    }
    outputs = {
      host = module.svc.host
      port = module.svc.port
      url  = module.svc.url
    }
  }
}
`)

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
  cache:
    type: redis:^7

deployments:
  api:
    image: api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      REDIS_URL: "${{ databases.cache.url }}"
      AUTH_URL: "${{ services.auth.url }}"
  worker:
    image: worker:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"

services:
  auth:
    deployment: api
    port: 8080
`)

	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	// Expected: 2 databases + 2 deployments + 1 service = 5 core nodes
	// + databaseUser: main--api, cache--api, main--worker = 3 databaseUser nodes
	// + networkPolicy: api--auth (api references service auth) = 1 networkPolicy node
	// = 9 total nodes
	assert.Equal(t, 9, len(g.Nodes),
		"expected 9 nodes: 5 core + 3 databaseUser + 1 networkPolicy")

	// Verify networkPolicy node
	npNode := g.GetNode("app/networkPolicy/api--auth")
	require.NotNil(t, npNode)
	assert.Equal(t, NodeTypeNetworkPolicy, npNode.Type)

	// networkPolicy should depend on both the workload and the service
	assert.Contains(t, npNode.DependsOn, "app/deployment/api")
	assert.Contains(t, npNode.DependsOn, "app/service/auth")
}

// ---------------------------------------------------------------------------
// When-clause filtering tests — verify that implicit nodes are only created
// when a hook's when condition matches the database/service type.
// ---------------------------------------------------------------------------

func TestIntegration_WhenFilter_OnlyPostgresGetsDbUser(t *testing.T) {
	// databaseUser hook only matches postgres. Component has postgres, redis
	// and mysql. Only postgres should get databaseUser nodes.
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = "h"
      port = "0"
      url  = "u"
    }
  }
  databaseUser {
    when = element(split(":", node.inputs.type), 0) == "postgres"
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = { name = "test" }
    }
    outputs = { url = "u" }
  }
  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = { id = "1" }
  }
}
`)
	comp := loadComponent(t, `
databases:
  pg:
    type: postgres:^16
  cache:
    type: redis:^7
  legacy:
    type: mysql:^8

deployments:
  api:
    image: api:latest
    environment:
      PG_URL: "${{ databases.pg.url }}"
      REDIS_URL: "${{ databases.cache.url }}"
      MYSQL_URL: "${{ databases.legacy.url }}"
`)
	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	// Only postgres should get a databaseUser node
	assert.NotNil(t, g.GetNode("app/databaseUser/pg--api"), "postgres should get databaseUser node")
	assert.Nil(t, g.GetNode("app/databaseUser/cache--api"), "redis should NOT get databaseUser node")
	assert.Nil(t, g.GetNode("app/databaseUser/legacy--api"), "mysql should NOT get databaseUser node")

	// Deployment depends on databaseUser for postgres, directly on others
	apiNode := g.GetNode("app/deployment/api")
	require.NotNil(t, apiNode)
	assert.Contains(t, apiNode.DependsOn, "app/databaseUser/pg--api")
	assert.Contains(t, apiNode.DependsOn, "app/database/cache")
	assert.Contains(t, apiNode.DependsOn, "app/database/legacy")
	assert.NotContains(t, apiNode.DependsOn, "app/database/pg")

	// 3 databases + 1 databaseUser + 1 deployment = 5
	sorted, err := g.TopologicalSort()
	require.NoError(t, err)
	assert.Len(t, sorted, 5)
}

func TestIntegration_WhenFilter_CatchAllHookMatchesAll(t *testing.T) {
	// databaseUser hook without a when clause (catch-all) should create
	// nodes for ALL database types.
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = "h"
      port = "0"
      url  = "u"
    }
  }
  databaseUser {
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = { name = "test" }
    }
    outputs = { url = "u" }
  }
  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = { id = "1" }
  }
}
`)
	comp := loadComponent(t, `
databases:
  pg:
    type: postgres:^16
  cache:
    type: redis:^7

deployments:
  api:
    image: api:latest
    environment:
      PG_URL: "${{ databases.pg.url }}"
      REDIS_URL: "${{ databases.cache.url }}"
`)
	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	// Catch-all: both databases get databaseUser nodes
	assert.NotNil(t, g.GetNode("app/databaseUser/pg--api"), "postgres should get databaseUser node")
	assert.NotNil(t, g.GetNode("app/databaseUser/cache--api"), "redis should get databaseUser node (catch-all)")

	apiNode := g.GetNode("app/deployment/api")
	require.NotNil(t, apiNode)
	assert.NotContains(t, apiNode.DependsOn, "app/database/pg")
	assert.NotContains(t, apiNode.DependsOn, "app/database/cache")
}

func TestIntegration_WhenFilter_MultipleHooksMatchDifferentTypes(t *testing.T) {
	// Two databaseUser hooks: one for postgres, one for redis. Both should
	// create nodes for their respective types, but mysql should be skipped.
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = "h"
      port = "0"
      url  = "u"
    }
  }
  databaseUser {
    when = element(split(":", node.inputs.type), 0) == "postgres"
    module "pg_user" {
      plugin = "native"
      build  = "./modules/pg-user"
      inputs = { name = "test" }
    }
    outputs = { url = "u" }
  }
  databaseUser {
    when = element(split(":", node.inputs.type), 0) == "redis"
    module "redis_user" {
      plugin = "native"
      build  = "./modules/redis-user"
      inputs = { name = "test" }
    }
    outputs = { url = "u" }
  }
  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = { id = "1" }
  }
}
`)
	comp := loadComponent(t, `
databases:
  pg:
    type: postgres:^16
  cache:
    type: redis:^7
  docs:
    type: mongodb:^7

deployments:
  api:
    image: api:latest
    environment:
      PG_URL: "${{ databases.pg.url }}"
      REDIS_URL: "${{ databases.cache.url }}"
      MONGO_URL: "${{ databases.docs.url }}"
`)
	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	// Postgres and redis match their respective hooks
	assert.NotNil(t, g.GetNode("app/databaseUser/pg--api"), "postgres matches first hook")
	assert.NotNil(t, g.GetNode("app/databaseUser/cache--api"), "redis matches second hook")
	// MongoDB matches neither hook
	assert.Nil(t, g.GetNode("app/databaseUser/docs--api"), "mongodb matches no hook — no databaseUser node")

	apiNode := g.GetNode("app/deployment/api")
	require.NotNil(t, apiNode)
	assert.Contains(t, apiNode.DependsOn, "app/databaseUser/pg--api")
	assert.Contains(t, apiNode.DependsOn, "app/databaseUser/cache--api")
	assert.Contains(t, apiNode.DependsOn, "app/database/docs", "mongodb connects directly to database")
}

func TestIntegration_WhenFilter_MultipleConsumersSameDatabase(t *testing.T) {
	// Two deployments both reference the same postgres database. The
	// databaseUser hook scoped to postgres should create separate
	// databaseUser nodes for each consumer.
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = "h"
      port = "0"
      url  = "u"
    }
  }
  databaseUser {
    when = element(split(":", node.inputs.type), 0) == "postgres"
    module "db_user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = { name = "test" }
    }
    outputs = { url = "u" }
  }
  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = { id = "1" }
  }
}
`)
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
  cache:
    type: redis:^7

deployments:
  api:
    image: api:latest
    environment:
      DB_URL: "${{ databases.main.url }}"
      REDIS_URL: "${{ databases.cache.url }}"
  worker:
    image: worker:latest
    environment:
      DB_URL: "${{ databases.main.url }}"
      REDIS_URL: "${{ databases.cache.url }}"
`)
	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	// Both consumers get databaseUser nodes for postgres
	assert.NotNil(t, g.GetNode("app/databaseUser/main--api"), "api should get postgres databaseUser")
	assert.NotNil(t, g.GetNode("app/databaseUser/main--worker"), "worker should get postgres databaseUser")

	// Neither consumer gets databaseUser for redis
	assert.Nil(t, g.GetNode("app/databaseUser/cache--api"), "api should NOT get redis databaseUser")
	assert.Nil(t, g.GetNode("app/databaseUser/cache--worker"), "worker should NOT get redis databaseUser")

	// Both deployments should depend directly on redis
	apiNode := g.GetNode("app/deployment/api")
	workerNode := g.GetNode("app/deployment/worker")
	require.NotNil(t, apiNode)
	require.NotNil(t, workerNode)
	assert.Contains(t, apiNode.DependsOn, "app/database/cache")
	assert.Contains(t, workerNode.DependsOn, "app/database/cache")

	// 2 databases + 2 databaseUser (postgres only) + 2 deployments = 6
	sorted, err := g.TopologicalSort()
	require.NoError(t, err)
	assert.Len(t, sorted, 6)
}

func TestIntegration_WhenFilter_NoDatabaseUserHooks(t *testing.T) {
	// No databaseUser hooks at all — all deployments should connect
	// directly to database nodes with no databaseUser interposition.
	dc := loadDatacenter(t, `
environment {
  database {
    module "db" {
      plugin = "native"
      build  = "./modules/db"
      inputs = { name = "test" }
    }
    outputs = {
      host = "h"
      port = "0"
      url  = "u"
    }
  }
  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
      inputs = { name = "test" }
    }
    outputs = { id = "1" }
  }
}
`)
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: api:latest
    environment:
      DB_URL: "${{ databases.main.url }}"
`)
	g := buildGraphFromDatacenter(t, dc, map[string]component.Component{"app": comp})

	// No databaseUser nodes should exist
	assert.Nil(t, g.GetNode("app/databaseUser/main--api"))

	// Deployment depends directly on database
	apiNode := g.GetNode("app/deployment/api")
	require.NotNil(t, apiNode)
	assert.Contains(t, apiNode.DependsOn, "app/database/main")

	// 1 database + 1 deployment = 2
	sorted, err := g.TopologicalSort()
	require.NoError(t, err)
	assert.Len(t, sorted, 2)
}
