package graph

import (
	"testing"

	"github.com/davidthor/cldctl/pkg/schema/component"
)

// loadComponent is a test helper that parses a YAML component specification.
func loadComponent(t *testing.T, yaml string) component.Component {
	t.Helper()
	loader := component.NewLoader()
	comp, err := loader.LoadFromBytes([]byte(yaml), "/tmp/test/cloud.component.yml")
	if err != nil {
		t.Fatalf("failed to load component: %v", err)
	}
	return comp
}

func TestBuilder_AddComponent_TaskFromMigration(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
    migrations:
      image: my-app-migrations:latest
      command: ["npm", "run", "migrate"]
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have database node
	dbNode := g.GetNode("my-app/database/main")
	if dbNode == nil {
		t.Fatal("expected database node to exist")
	}
	if dbNode.Type != NodeTypeDatabase {
		t.Errorf("expected database node type, got %s", dbNode.Type)
	}

	// Should have task node (not migration node)
	taskNode := g.GetNode("my-app/task/main-migration")
	if taskNode == nil {
		t.Fatal("expected task node to exist")
	}
	if taskNode.Type != NodeTypeTask {
		t.Errorf("expected task node type, got %s", taskNode.Type)
	}

	// Task should depend on database
	hasDep := false
	for _, dep := range taskNode.DependsOn {
		if dep == dbNode.ID {
			hasDep = true
			break
		}
	}
	if !hasDep {
		t.Error("expected task node to depend on database node")
	}
}

func TestBuilder_TaskDependencyInsertion(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")
	builder.EnableImplicitNodes(true, false)

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
    migrations:
      image: my-app-migrations:latest
      command: ["npm", "run", "migrate"]

deployments:
  api:
    image: my-app:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"

functions:
  web:
    src:
      path: ./web
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	dbNode := g.GetNode("my-app/database/main")
	taskNode := g.GetNode("my-app/task/main-migration")
	deployNode := g.GetNode("my-app/deployment/api")
	fnNode := g.GetNode("my-app/function/web")

	// databaseUser nodes should be interposed
	dbUserAPI := g.GetNode("my-app/databaseUser/main--api")
	dbUserWeb := g.GetNode("my-app/databaseUser/main--web")

	if dbNode == nil || taskNode == nil || deployNode == nil || fnNode == nil {
		t.Fatal("expected all nodes to exist")
	}
	if dbUserAPI == nil || dbUserWeb == nil {
		t.Fatal("expected databaseUser nodes to exist")
	}

	// Deployment should depend on task (not just database)
	deployDependsOnTask := false
	for _, dep := range deployNode.DependsOn {
		if dep == taskNode.ID {
			deployDependsOnTask = true
			break
		}
	}
	if !deployDependsOnTask {
		t.Error("expected deployment to depend on task node")
	}

	// Function should depend on task (not just database)
	fnDependsOnTask := false
	for _, dep := range fnNode.DependsOn {
		if dep == taskNode.ID {
			fnDependsOnTask = true
			break
		}
	}
	if !fnDependsOnTask {
		t.Error("expected function to depend on task node")
	}

	// Deployment should depend on databaseUser (not database directly)
	deployDependsOnDBUser := false
	for _, dep := range deployNode.DependsOn {
		if dep == dbUserAPI.ID {
			deployDependsOnDBUser = true
			break
		}
	}
	if !deployDependsOnDBUser {
		t.Error("expected deployment to depend on databaseUser node")
	}

	// databaseUser should depend on database
	dbUserDependsOnDB := false
	for _, dep := range dbUserAPI.DependsOn {
		if dep == dbNode.ID {
			dbUserDependsOnDB = true
			break
		}
	}
	if !dbUserDependsOnDB {
		t.Error("expected databaseUser to depend on database node")
	}

	// Task should depend on database
	taskDependsOnDB := false
	for _, dep := range taskNode.DependsOn {
		if dep == dbNode.ID {
			taskDependsOnDB = true
			break
		}
	}
	if !taskDependsOnDB {
		t.Error("expected task to depend on database node")
	}

	// Verify topological sort produces correct order: database -> task/databaseUser -> deployment/function
	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected topological sort error: %v", err)
	}

	nodeIndex := make(map[string]int)
	for i, n := range sorted {
		nodeIndex[n.ID] = i
	}

	if nodeIndex[dbNode.ID] >= nodeIndex[taskNode.ID] {
		t.Error("database should come before task in topological order")
	}
	if nodeIndex[taskNode.ID] >= nodeIndex[deployNode.ID] {
		t.Error("task should come before deployment in topological order")
	}
	if nodeIndex[taskNode.ID] >= nodeIndex[fnNode.ID] {
		t.Error("task should come before function in topological order")
	}
	if nodeIndex[dbNode.ID] >= nodeIndex[dbUserAPI.ID] {
		t.Error("database should come before databaseUser in topological order")
	}
	if nodeIndex[dbUserAPI.ID] >= nodeIndex[deployNode.ID] {
		t.Error("databaseUser should come before deployment in topological order")
	}
}

func TestBuilder_NoTaskNoDependencyInsertion(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")
	builder.EnableImplicitNodes(true, false)

	// Database WITHOUT migrations
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: my-app:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// No task node should exist
	taskNodes := g.GetNodesByType(NodeTypeTask)
	if len(taskNodes) != 0 {
		t.Errorf("expected 0 task nodes, got %d", len(taskNodes))
	}

	// Deployment should depend on databaseUser (not database directly)
	deployNode := g.GetNode("my-app/deployment/api")
	if deployNode == nil {
		t.Fatal("expected deployment node to exist")
	}

	dbUserNode := g.GetNode("my-app/databaseUser/main--api")
	if dbUserNode == nil {
		t.Fatal("expected databaseUser node to exist")
	}

	deployDependsOnDBUser := false
	for _, dep := range deployNode.DependsOn {
		if dep == dbUserNode.ID {
			deployDependsOnDBUser = true
			break
		}
	}
	if !deployDependsOnDBUser {
		t.Error("expected deployment to depend on databaseUser node")
	}

	// databaseUser should depend on database
	dbUserDependsOnDB := false
	for _, dep := range dbUserNode.DependsOn {
		if dep == "my-app/database/main" {
			dbUserDependsOnDB = true
			break
		}
	}
	if !dbUserDependsOnDB {
		t.Error("expected databaseUser to depend on database node")
	}
}

func TestBuilder_CronjobDependsOnTask(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
    migrations:
      image: my-app-migrations:latest
      command: ["npm", "run", "migrate"]

cronjobs:
  cleanup:
    image: my-app-cleanup:latest
    schedule: "0 2 * * *"
    command: ["npm", "run", "cleanup"]
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	taskNode := g.GetNode("my-app/task/main-migration")
	cronNode := g.GetNode("my-app/cronjob/cleanup")

	if taskNode == nil || cronNode == nil {
		t.Fatal("expected task and cronjob nodes to exist")
	}

	// Cronjob should depend on task
	cronDependsOnTask := false
	for _, dep := range cronNode.DependsOn {
		if dep == taskNode.ID {
			cronDependsOnTask = true
			break
		}
	}
	if !cronDependsOnTask {
		t.Error("expected cronjob to depend on task node")
	}
}

func TestBuilder_TaskFromMigration_WithRuntime(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
    migrations:
      runtime: node:20
      command: ["npx", "prisma", "migrate", "deploy"]
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have task node
	taskNode := g.GetNode("my-app/task/main-migration")
	if taskNode == nil {
		t.Fatal("expected task node to exist")
	}

	// Task should have runtime input
	if taskNode.Inputs["runtime"] == nil {
		t.Error("expected task node to have runtime input")
	}

	// Task should have workingDirectory input
	if taskNode.Inputs["workingDirectory"] == nil {
		t.Error("expected task node to have workingDirectory input")
	}

	// Task should NOT have image input
	if taskNode.Inputs["image"] != nil {
		t.Error("expected task node to NOT have image input")
	}

	// No dockerBuild node should exist (runtime-based, no inline builds)
	buildNodes := g.GetNodesByType(NodeTypeDockerBuild)
	if len(buildNodes) != 0 {
		t.Errorf("expected 0 dockerBuild nodes, got %d", len(buildNodes))
	}
}

func TestBuilder_TaskFromMigration_ProcessBased(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	// Migration with no image and no runtime — bare process execution
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
    migrations:
      command: ["npm", "run", "migrate"]
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	taskNode := g.GetNode("my-app/task/main-migration")
	if taskNode == nil {
		t.Fatal("expected task node to exist")
	}

	// Task should have workingDirectory defaulted to component directory
	if taskNode.Inputs["workingDirectory"] == nil {
		t.Error("expected task node to have workingDirectory input")
	}

	// No image or runtime
	if taskNode.Inputs["image"] != nil {
		t.Error("expected task node to NOT have image input")
	}
	if taskNode.Inputs["runtime"] != nil {
		t.Error("expected task node to NOT have runtime input")
	}
}

// === Tests for implicit databaseUser nodes ===

func TestBuilder_DatabaseUserNode_NotCreatedWithoutHook(t *testing.T) {
	// When hasDatabaseUserHook is false (default), no databaseUser nodes should be created.
	// The deployment should depend directly on the database.
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: my-app:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should NOT have any databaseUser nodes
	dbUserNodes := g.GetNodesByType(NodeTypeDatabaseUser)
	if len(dbUserNodes) != 0 {
		t.Errorf("expected 0 databaseUser nodes when hook is not enabled, got %d", len(dbUserNodes))
	}

	// Deployment should depend directly on database
	deployNode := g.GetNode("my-app/deployment/api")
	dbNode := g.GetNode("my-app/database/main")
	if deployNode == nil || dbNode == nil {
		t.Fatal("expected both deployment and database nodes to exist")
	}

	deployDependsOnDB := false
	for _, dep := range deployNode.DependsOn {
		if dep == dbNode.ID {
			deployDependsOnDB = true
			break
		}
	}
	if !deployDependsOnDB {
		t.Error("expected deployment to depend directly on database when no databaseUser hook")
	}
}

func TestBuilder_DatabaseUserNode_SingleDeployment(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")
	builder.EnableImplicitNodes(true, false)

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: my-app:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have a databaseUser node
	dbUserNode := g.GetNode("my-app/databaseUser/main--api")
	if dbUserNode == nil {
		t.Fatal("expected databaseUser node to exist")
	}
	if dbUserNode.Type != NodeTypeDatabaseUser {
		t.Errorf("expected databaseUser type, got %s", dbUserNode.Type)
	}

	// Verify inputs
	if dbUserNode.Inputs["database"] != "main" {
		t.Errorf("expected database input 'main', got %v", dbUserNode.Inputs["database"])
	}
	if dbUserNode.Inputs["consumer"] != "api" {
		t.Errorf("expected consumer input 'api', got %v", dbUserNode.Inputs["consumer"])
	}
	if dbUserNode.Inputs["consumerType"] != "deployment" {
		t.Errorf("expected consumerType input 'deployment', got %v", dbUserNode.Inputs["consumerType"])
	}

	// databaseUser should depend on database
	dbNode := g.GetNode("my-app/database/main")
	if dbNode == nil {
		t.Fatal("expected database node to exist")
	}

	dbUserDependsOnDB := false
	for _, dep := range dbUserNode.DependsOn {
		if dep == dbNode.ID {
			dbUserDependsOnDB = true
			break
		}
	}
	if !dbUserDependsOnDB {
		t.Error("expected databaseUser to depend on database")
	}

	// Deployment should depend on databaseUser
	deployNode := g.GetNode("my-app/deployment/api")
	if deployNode == nil {
		t.Fatal("expected deployment node to exist")
	}

	deployDependsOnDBUser := false
	for _, dep := range deployNode.DependsOn {
		if dep == dbUserNode.ID {
			deployDependsOnDBUser = true
			break
		}
	}
	if !deployDependsOnDBUser {
		t.Error("expected deployment to depend on databaseUser, not database directly")
	}

	// Deployment should NOT depend directly on database
	deployDependsOnDB := false
	for _, dep := range deployNode.DependsOn {
		if dep == dbNode.ID {
			deployDependsOnDB = true
			break
		}
	}
	if deployDependsOnDB {
		t.Error("deployment should NOT depend directly on database (should go through databaseUser)")
	}
}

func TestBuilder_DatabaseUserNode_TwoConsumers(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")
	builder.EnableImplicitNodes(true, false)

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: my-app-api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
  admin:
    image: my-app-admin:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have two separate databaseUser nodes
	dbUserAPI := g.GetNode("my-app/databaseUser/main--api")
	dbUserAdmin := g.GetNode("my-app/databaseUser/main--admin")

	if dbUserAPI == nil {
		t.Fatal("expected databaseUser/main--api node to exist")
	}
	if dbUserAdmin == nil {
		t.Fatal("expected databaseUser/main--admin node to exist")
	}

	// Both should depend on the same database
	dbNode := g.GetNode("my-app/database/main")
	if dbNode == nil {
		t.Fatal("expected database node to exist")
	}

	for _, dbUser := range []*Node{dbUserAPI, dbUserAdmin} {
		found := false
		for _, dep := range dbUser.DependsOn {
			if dep == dbNode.ID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s to depend on database", dbUser.ID)
		}
	}

	// Total databaseUser nodes should be exactly 2
	dbUserNodes := g.GetNodesByType(NodeTypeDatabaseUser)
	if len(dbUserNodes) != 2 {
		t.Errorf("expected 2 databaseUser nodes, got %d", len(dbUserNodes))
	}
}

func TestBuilder_DatabaseUserNode_NoDuplicates(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")
	builder.EnableImplicitNodes(true, false)

	// A deployment referencing the same database twice should still produce only one databaseUser node
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: my-app:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      DATABASE_HOST: "${{ databases.main.host }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have exactly one databaseUser node
	dbUserNodes := g.GetNodesByType(NodeTypeDatabaseUser)
	if len(dbUserNodes) != 1 {
		t.Errorf("expected 1 databaseUser node (no duplicates), got %d", len(dbUserNodes))
	}
}

func TestBuilder_DatabaseUserNode_FunctionConsumer(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")
	builder.EnableImplicitNodes(true, false)

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

functions:
  web:
    src:
      path: ./web
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	dbUserNode := g.GetNode("my-app/databaseUser/main--web")
	if dbUserNode == nil {
		t.Fatal("expected databaseUser node for function consumer")
	}

	if dbUserNode.Inputs["consumerType"] != "function" {
		t.Errorf("expected consumerType 'function', got %v", dbUserNode.Inputs["consumerType"])
	}
}

func TestBuilder_DatabaseUserNode_CronjobConsumer(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")
	builder.EnableImplicitNodes(true, false)

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

cronjobs:
  cleanup:
    image: my-app-cleanup:latest
    schedule: "0 2 * * *"
    command: ["npm", "run", "cleanup"]
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	dbUserNode := g.GetNode("my-app/databaseUser/main--cleanup")
	if dbUserNode == nil {
		t.Fatal("expected databaseUser node for cronjob consumer")
	}

	if dbUserNode.Inputs["consumerType"] != "cronjob" {
		t.Errorf("expected consumerType 'cronjob', got %v", dbUserNode.Inputs["consumerType"])
	}
}

// === Tests for implicit networkPolicy nodes ===

func TestBuilder_NetworkPolicyNode_NotCreatedWithoutHook(t *testing.T) {
	// When hasNetworkPolicyHook is false (default), no networkPolicy nodes should be created.
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
deployments:
  auth:
    image: auth:latest
  api:
    image: api:latest
    environment:
      AUTH_URL: "${{ services.auth.url }}"

services:
  auth:
    deployment: auth
    port: 8080
  api:
    deployment: api
    port: 3001
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should NOT have any networkPolicy nodes
	npNodes := g.GetNodesByType(NodeTypeNetworkPolicy)
	if len(npNodes) != 0 {
		t.Errorf("expected 0 networkPolicy nodes when hook is not enabled, got %d", len(npNodes))
	}
}

func TestBuilder_NetworkPolicyNode_DeploymentReferencesService(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")
	builder.EnableImplicitNodes(false, true)

	comp := loadComponent(t, `
deployments:
  auth:
    image: auth:latest
  api:
    image: api:latest
    environment:
      AUTH_URL: "${{ services.auth.url }}"

services:
  auth:
    deployment: auth
    port: 8080
  api:
    deployment: api
    port: 3001
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have a networkPolicy node
	npNode := g.GetNode("my-app/networkPolicy/api--auth")
	if npNode == nil {
		t.Fatal("expected networkPolicy node to exist")
	}
	if npNode.Type != NodeTypeNetworkPolicy {
		t.Errorf("expected networkPolicy type, got %s", npNode.Type)
	}

	// Verify inputs
	if npNode.Inputs["from"] != "api" {
		t.Errorf("expected 'from' input 'api', got %v", npNode.Inputs["from"])
	}
	if npNode.Inputs["fromType"] != "deployment" {
		t.Errorf("expected 'fromType' input 'deployment', got %v", npNode.Inputs["fromType"])
	}
	if npNode.Inputs["to"] != "auth" {
		t.Errorf("expected 'to' input 'auth', got %v", npNode.Inputs["to"])
	}
	if npNode.Inputs["port"] != "8080" {
		t.Errorf("expected 'port' input '8080', got %v", npNode.Inputs["port"])
	}

	// networkPolicy should depend on both api deployment and auth service
	deployNode := g.GetNode("my-app/deployment/api")
	svcNode := g.GetNode("my-app/service/auth")

	npDepsOnDeploy := false
	npDepsOnSvc := false
	for _, dep := range npNode.DependsOn {
		if dep == deployNode.ID {
			npDepsOnDeploy = true
		}
		if dep == svcNode.ID {
			npDepsOnSvc = true
		}
	}
	if !npDepsOnDeploy {
		t.Error("expected networkPolicy to depend on api deployment")
	}
	if !npDepsOnSvc {
		t.Error("expected networkPolicy to depend on auth service")
	}

	// Nothing should depend on the networkPolicy (it's a leaf node)
	if len(npNode.DependedOnBy) != 0 {
		t.Errorf("expected no dependents on networkPolicy, got %d: %v", len(npNode.DependedOnBy), npNode.DependedOnBy)
	}
}

func TestBuilder_NetworkPolicyNode_NoDuplicates(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")
	builder.EnableImplicitNodes(false, true)

	comp := loadComponent(t, `
deployments:
  api:
    image: api:latest
    environment:
      AUTH_URL: "${{ services.auth.url }}"
      AUTH_HOST: "${{ services.auth.host }}"

services:
  auth:
    deployment: api
    port: 8080
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have exactly one networkPolicy node (no duplicates)
	npNodes := g.GetNodesByType(NodeTypeNetworkPolicy)
	if len(npNodes) != 1 {
		t.Errorf("expected 1 networkPolicy node, got %d", len(npNodes))
	}
}

func TestBuilder_NetworkPolicyNode_NoNetworkPolicyForNonServiceRefs(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")
	builder.EnableImplicitNodes(false, true)

	// Deployment referencing a database — should NOT create a networkPolicy
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: my-app:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	npNodes := g.GetNodesByType(NodeTypeNetworkPolicy)
	if len(npNodes) != 0 {
		t.Errorf("expected 0 networkPolicy nodes for database references, got %d", len(npNodes))
	}
}

func TestBuilder_TopologicalSort_WithImplicitNodes(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")
	builder.EnableImplicitNodes(true, true)

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  auth:
    image: auth:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
  api:
    image: api:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      AUTH_URL: "${{ services.auth.url }}"

services:
  auth:
    deployment: auth
    port: 8080
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Topological sort should succeed (no cycles)
	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected topological sort error: %v", err)
	}

	nodeIndex := make(map[string]int)
	for i, n := range sorted {
		nodeIndex[n.ID] = i
	}

	dbNode := g.GetNode("my-app/database/main")
	dbUserAuth := g.GetNode("my-app/databaseUser/main--auth")
	dbUserAPI := g.GetNode("my-app/databaseUser/main--api")
	authDeploy := g.GetNode("my-app/deployment/auth")
	apiDeploy := g.GetNode("my-app/deployment/api")
	npNode := g.GetNode("my-app/networkPolicy/api--auth")

	if dbNode == nil || dbUserAuth == nil || dbUserAPI == nil || authDeploy == nil || apiDeploy == nil {
		t.Fatal("expected all core nodes to exist")
	}

	// database must come before databaseUser nodes
	if nodeIndex[dbNode.ID] >= nodeIndex[dbUserAuth.ID] {
		t.Error("database should come before databaseUser/main--auth")
	}
	if nodeIndex[dbNode.ID] >= nodeIndex[dbUserAPI.ID] {
		t.Error("database should come before databaseUser/main--api")
	}

	// databaseUser nodes must come before their consumers
	if nodeIndex[dbUserAuth.ID] >= nodeIndex[authDeploy.ID] {
		t.Error("databaseUser/main--auth should come before deployment/auth")
	}
	if nodeIndex[dbUserAPI.ID] >= nodeIndex[apiDeploy.ID] {
		t.Error("databaseUser/main--api should come before deployment/api")
	}

	// networkPolicy is a leaf but should come after its dependencies
	if npNode != nil {
		if nodeIndex[apiDeploy.ID] >= nodeIndex[npNode.ID] {
			t.Error("api deployment should come before networkPolicy")
		}
	}
}

// === Tests for encryption key nodes ===

func TestBuilder_EncryptionKeyNode_Created(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
encryptionKeys:
  app_secret:
    type: symmetric
    bytes: 32
  signing:
    type: rsa
    bits: 2048
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have two encryptionKey nodes
	ekNodes := g.GetNodesByType(NodeTypeEncryptionKey)
	if len(ekNodes) != 2 {
		t.Fatalf("expected 2 encryptionKey nodes, got %d", len(ekNodes))
	}

	// Verify symmetric key node
	symNode := g.GetNode("my-app/encryptionKey/app_secret")
	if symNode == nil {
		t.Fatal("expected encryptionKey/app_secret node to exist")
	}
	if symNode.Type != NodeTypeEncryptionKey {
		t.Errorf("expected encryptionKey type, got %s", symNode.Type)
	}
	if symNode.Inputs["keyType"] != "symmetric" {
		t.Errorf("expected keyType 'symmetric', got %v", symNode.Inputs["keyType"])
	}
	if symNode.Inputs["keySize"] != 32 {
		t.Errorf("expected keySize 32, got %v", symNode.Inputs["keySize"])
	}

	// Verify RSA key node
	rsaNode := g.GetNode("my-app/encryptionKey/signing")
	if rsaNode == nil {
		t.Fatal("expected encryptionKey/signing node to exist")
	}
	if rsaNode.Inputs["keyType"] != "rsa" {
		t.Errorf("expected keyType 'rsa', got %v", rsaNode.Inputs["keyType"])
	}
	if rsaNode.Inputs["keySize"] != 2048 {
		t.Errorf("expected keySize 2048, got %v", rsaNode.Inputs["keySize"])
	}
}

func TestBuilder_EncryptionKeyNode_DeploymentDependency(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
encryptionKeys:
  app_secret:
    type: symmetric
    bytes: 32

deployments:
  server:
    image: my-app:latest
    environment:
      APP_SECRET: "${{ encryptionKeys.app_secret.keyBase64 }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	ekNode := g.GetNode("my-app/encryptionKey/app_secret")
	deployNode := g.GetNode("my-app/deployment/server")

	if ekNode == nil {
		t.Fatal("expected encryptionKey node to exist")
	}
	if deployNode == nil {
		t.Fatal("expected deployment node to exist")
	}

	// Deployment should depend on encryptionKey
	deployDependsOnEK := false
	for _, dep := range deployNode.DependsOn {
		if dep == ekNode.ID {
			deployDependsOnEK = true
			break
		}
	}
	if !deployDependsOnEK {
		t.Error("expected deployment to depend on encryptionKey node")
	}

	// encryptionKey should list deployment as a dependent
	ekHasDependent := false
	for _, depBy := range ekNode.DependedOnBy {
		if depBy == deployNode.ID {
			ekHasDependent = true
			break
		}
	}
	if !ekHasDependent {
		t.Error("expected encryptionKey to list deployment as a dependent")
	}
}

// === Tests for SMTP nodes ===

func TestBuilder_SMTPNode_Created(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
smtp:
  email:
    description: "SMTP server for system emails"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have one SMTP node
	smtpNodes := g.GetNodesByType(NodeTypeSMTP)
	if len(smtpNodes) != 1 {
		t.Fatalf("expected 1 smtp node, got %d", len(smtpNodes))
	}

	smtpNode := g.GetNode("my-app/smtp/email")
	if smtpNode == nil {
		t.Fatal("expected smtp/email node to exist")
	}
	if smtpNode.Type != NodeTypeSMTP {
		t.Errorf("expected smtp type, got %s", smtpNode.Type)
	}
	if smtpNode.Inputs["description"] != "SMTP server for system emails" {
		t.Errorf("expected description input, got %v", smtpNode.Inputs["description"])
	}
}

func TestBuilder_SMTPNode_DeploymentDependency(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
smtp:
  email:
    description: "SMTP server for system emails"

deployments:
  server:
    image: my-app:latest
    environment:
      SMTP_HOST: "${{ smtp.email.host }}"
      SMTP_PORT: "${{ smtp.email.port }}"
      SMTP_USER: "${{ smtp.email.username }}"
      SMTP_PASS: "${{ smtp.email.password }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	smtpNode := g.GetNode("my-app/smtp/email")
	deployNode := g.GetNode("my-app/deployment/server")

	if smtpNode == nil {
		t.Fatal("expected smtp node to exist")
	}
	if deployNode == nil {
		t.Fatal("expected deployment node to exist")
	}

	// Deployment should depend on smtp
	deployDependsOnSMTP := false
	for _, dep := range deployNode.DependsOn {
		if dep == smtpNode.ID {
			deployDependsOnSMTP = true
			break
		}
	}
	if !deployDependsOnSMTP {
		t.Error("expected deployment to depend on smtp node")
	}

	// smtp should list deployment as a dependent
	smtpHasDependent := false
	for _, depBy := range smtpNode.DependedOnBy {
		if depBy == deployNode.ID {
			smtpHasDependent = true
			break
		}
	}
	if !smtpHasDependent {
		t.Error("expected smtp to list deployment as a dependent")
	}
}

func TestBuilder_EncryptionKeyAndSMTP_TopologicalOrder(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
encryptionKeys:
  app_secret:
    type: symmetric
    bytes: 32

smtp:
  email:
    description: "SMTP server"

databases:
  main:
    type: postgres:^16

deployments:
  server:
    image: my-app:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
      APP_SECRET: "${{ encryptionKeys.app_secret.keyBase64 }}"
      SMTP_HOST: "${{ smtp.email.host }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Topological sort should succeed (no cycles)
	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected topological sort error: %v", err)
	}

	nodeIndex := make(map[string]int)
	for i, n := range sorted {
		nodeIndex[n.ID] = i
	}

	dbNode := g.GetNode("my-app/database/main")
	ekNode := g.GetNode("my-app/encryptionKey/app_secret")
	smtpNode := g.GetNode("my-app/smtp/email")
	deployNode := g.GetNode("my-app/deployment/server")

	if dbNode == nil || ekNode == nil || smtpNode == nil || deployNode == nil {
		t.Fatal("expected all nodes to exist")
	}

	// All dependencies must come before the deployment
	if nodeIndex[dbNode.ID] >= nodeIndex[deployNode.ID] {
		t.Error("database should come before deployment in topological order")
	}
	if nodeIndex[ekNode.ID] >= nodeIndex[deployNode.ID] {
		t.Error("encryptionKey should come before deployment in topological order")
	}
	if nodeIndex[smtpNode.ID] >= nodeIndex[deployNode.ID] {
		t.Error("smtp should come before deployment in topological order")
	}
}

func TestBuilder_EncryptionKeyAndSMTP_FunctionDependency(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
encryptionKeys:
  signing:
    type: rsa
    bits: 2048

smtp:
  notifications:
    description: "Notification emails"

functions:
  api:
    src:
      path: ./api
    environment:
      PRIVATE_KEY: "${{ encryptionKeys.signing.privateKeyBase64 }}"
      SMTP_HOST: "${{ smtp.notifications.host }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	ekNode := g.GetNode("my-app/encryptionKey/signing")
	smtpNode := g.GetNode("my-app/smtp/notifications")
	fnNode := g.GetNode("my-app/function/api")

	if ekNode == nil {
		t.Fatal("expected encryptionKey node to exist")
	}
	if smtpNode == nil {
		t.Fatal("expected smtp node to exist")
	}
	if fnNode == nil {
		t.Fatal("expected function node to exist")
	}

	// Function should depend on both encryptionKey and smtp
	fnDepsOnEK := false
	fnDepsOnSMTP := false
	for _, dep := range fnNode.DependsOn {
		if dep == ekNode.ID {
			fnDepsOnEK = true
		}
		if dep == smtpNode.ID {
			fnDepsOnSMTP = true
		}
	}
	if !fnDepsOnEK {
		t.Error("expected function to depend on encryptionKey node")
	}
	if !fnDepsOnSMTP {
		t.Error("expected function to depend on smtp node")
	}
}

func TestBuilder_NoEncryptionKeyOrSMTP_NoNodes(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	// Component with no encryption keys or smtp
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: my-app:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have no encryptionKey or smtp nodes
	ekNodes := g.GetNodesByType(NodeTypeEncryptionKey)
	if len(ekNodes) != 0 {
		t.Errorf("expected 0 encryptionKey nodes, got %d", len(ekNodes))
	}

	smtpNodes := g.GetNodesByType(NodeTypeSMTP)
	if len(smtpNodes) != 0 {
		t.Errorf("expected 0 smtp nodes, got %d", len(smtpNodes))
	}
}

func TestBuilder_EncryptionKeyAndSMTP_CronjobDependency(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
encryptionKeys:
  signing:
    type: rsa
    bits: 2048

smtp:
  email:
    description: "System emails"

cronjobs:
  notify:
    image: my-app-notify:latest
    schedule: "0 8 * * *"
    command: ["npm", "run", "notify"]
    environment:
      PRIVATE_KEY: "${{ encryptionKeys.signing.privateKey }}"
      SMTP_HOST: "${{ smtp.email.host }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	ekNode := g.GetNode("my-app/encryptionKey/signing")
	smtpNode := g.GetNode("my-app/smtp/email")
	cronNode := g.GetNode("my-app/cronjob/notify")

	if ekNode == nil {
		t.Fatal("expected encryptionKey node to exist")
	}
	if smtpNode == nil {
		t.Fatal("expected smtp node to exist")
	}
	if cronNode == nil {
		t.Fatal("expected cronjob node to exist")
	}

	// Cronjob should depend on both encryptionKey and smtp
	cronDepsOnEK := false
	cronDepsOnSMTP := false
	for _, dep := range cronNode.DependsOn {
		if dep == ekNode.ID {
			cronDepsOnEK = true
		}
		if dep == smtpNode.ID {
			cronDepsOnSMTP = true
		}
	}
	if !cronDepsOnEK {
		t.Error("expected cronjob to depend on encryptionKey node")
	}
	if !cronDepsOnSMTP {
		t.Error("expected cronjob to depend on smtp node")
	}
}

func TestBuilder_EncryptionKeyAndSMTP_WithInstances(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
encryptionKeys:
  app_secret:
    type: symmetric
    bytes: 32

smtp:
  email:
    description: "SMTP server"

deployments:
  server:
    image: my-app:latest
    environment:
      APP_SECRET: "${{ encryptionKeys.app_secret.keyBase64 }}"
      SMTP_HOST: "${{ smtp.email.host }}"

services:
  server:
    deployment: server
    port: 3000
`)

	instances := []InstanceInfo{
		{Name: "canary", Weight: 10},
		{Name: "stable", Weight: 90},
	}

	err := builder.AddComponentWithInstances("my-app", comp, instances, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Encryption key and SMTP nodes should be shared (one copy, not per-instance)
	ekNode := g.GetNode("my-app/encryptionKey/app_secret")
	smtpNode := g.GetNode("my-app/smtp/email")

	if ekNode == nil {
		t.Fatal("expected shared encryptionKey node to exist")
	}
	if smtpNode == nil {
		t.Fatal("expected shared smtp node to exist")
	}

	// Should have instance metadata on the shared nodes
	if len(ekNode.Instances) != 2 {
		t.Errorf("expected 2 instances on encryptionKey node, got %d", len(ekNode.Instances))
	}
	if len(smtpNode.Instances) != 2 {
		t.Errorf("expected 2 instances on smtp node, got %d", len(smtpNode.Instances))
	}

	// Should NOT have per-instance encryptionKey/smtp nodes
	ekNodes := g.GetNodesByType(NodeTypeEncryptionKey)
	if len(ekNodes) != 1 {
		t.Errorf("expected 1 encryptionKey node (shared), got %d", len(ekNodes))
	}

	smtpNodes := g.GetNodesByType(NodeTypeSMTP)
	if len(smtpNodes) != 1 {
		t.Errorf("expected 1 smtp node (shared), got %d", len(smtpNodes))
	}

	// Per-instance deployments should depend on the shared nodes
	canaryDeploy := g.GetNode("my-app/canary/deployment/server")
	stableDeploy := g.GetNode("my-app/stable/deployment/server")

	if canaryDeploy == nil {
		t.Fatal("expected canary deployment node to exist")
	}
	if stableDeploy == nil {
		t.Fatal("expected stable deployment node to exist")
	}

	for _, deployNode := range []*Node{canaryDeploy, stableDeploy} {
		depsOnEK := false
		depsOnSMTP := false
		for _, dep := range deployNode.DependsOn {
			if dep == ekNode.ID {
				depsOnEK = true
			}
			if dep == smtpNode.ID {
				depsOnSMTP = true
			}
		}
		if !depsOnEK {
			t.Errorf("expected %s to depend on encryptionKey node", deployNode.ID)
		}
		if !depsOnSMTP {
			t.Errorf("expected %s to depend on smtp node", deployNode.ID)
		}
	}
}

func TestBuilder_MultipleEncryptionKeys(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
encryptionKeys:
  app_secret:
    type: symmetric
    bytes: 32
  signing:
    type: rsa
    bits: 2048
  auth:
    type: ecdsa
    curve: P-256

deployments:
  server:
    image: my-app:latest
    environment:
      APP_SECRET: "${{ encryptionKeys.app_secret.keyBase64 }}"
      SIGNING_KEY: "${{ encryptionKeys.signing.privateKeyBase64 }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// All three encryption key nodes should be created
	ekNodes := g.GetNodesByType(NodeTypeEncryptionKey)
	if len(ekNodes) != 3 {
		t.Fatalf("expected 3 encryptionKey nodes, got %d", len(ekNodes))
	}

	// Verify each node exists with correct inputs
	symNode := g.GetNode("my-app/encryptionKey/app_secret")
	rsaNode := g.GetNode("my-app/encryptionKey/signing")
	ecdsaNode := g.GetNode("my-app/encryptionKey/auth")

	if symNode == nil || rsaNode == nil || ecdsaNode == nil {
		t.Fatal("expected all three encryptionKey nodes to exist")
	}

	if symNode.Inputs["keyType"] != "symmetric" {
		t.Errorf("expected keyType 'symmetric', got %v", symNode.Inputs["keyType"])
	}
	if symNode.Inputs["keySize"] != 32 {
		t.Errorf("expected keySize 32, got %v", symNode.Inputs["keySize"])
	}
	if rsaNode.Inputs["keyType"] != "rsa" {
		t.Errorf("expected keyType 'rsa', got %v", rsaNode.Inputs["keyType"])
	}
	if rsaNode.Inputs["keySize"] != 2048 {
		t.Errorf("expected keySize 2048, got %v", rsaNode.Inputs["keySize"])
	}
	if ecdsaNode.Inputs["keyType"] != "ecdsa" {
		t.Errorf("expected keyType 'ecdsa', got %v", ecdsaNode.Inputs["keyType"])
	}
	if ecdsaNode.Inputs["keySize"] != "P-256" {
		t.Errorf("expected keySize 'P-256', got %v", ecdsaNode.Inputs["keySize"])
	}

	// Deployment should depend on app_secret and signing (referenced in env), not auth (not referenced)
	deployNode := g.GetNode("my-app/deployment/server")
	if deployNode == nil {
		t.Fatal("expected deployment node to exist")
	}

	depsOnAppSecret := false
	depsOnSigning := false
	depsOnAuth := false
	for _, dep := range deployNode.DependsOn {
		switch dep {
		case symNode.ID:
			depsOnAppSecret = true
		case rsaNode.ID:
			depsOnSigning = true
		case ecdsaNode.ID:
			depsOnAuth = true
		}
	}
	if !depsOnAppSecret {
		t.Error("expected deployment to depend on encryptionKey/app_secret")
	}
	if !depsOnSigning {
		t.Error("expected deployment to depend on encryptionKey/signing")
	}
	if depsOnAuth {
		t.Error("deployment should NOT depend on encryptionKey/auth (not referenced in env)")
	}
}

func TestBuilder_MultipleSMTP(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
smtp:
  notifications:
    description: "Notification emails"
  transactional:
    description: "Transactional emails"

deployments:
  server:
    image: my-app:latest
    environment:
      NOTIFY_HOST: "${{ smtp.notifications.host }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Both SMTP nodes should be created
	smtpNodes := g.GetNodesByType(NodeTypeSMTP)
	if len(smtpNodes) != 2 {
		t.Fatalf("expected 2 smtp nodes, got %d", len(smtpNodes))
	}

	notifyNode := g.GetNode("my-app/smtp/notifications")
	txnNode := g.GetNode("my-app/smtp/transactional")

	if notifyNode == nil || txnNode == nil {
		t.Fatal("expected both smtp nodes to exist")
	}

	// Deployment should only depend on notifications (referenced in env), not transactional
	deployNode := g.GetNode("my-app/deployment/server")
	if deployNode == nil {
		t.Fatal("expected deployment node to exist")
	}

	depsOnNotify := false
	depsOnTxn := false
	for _, dep := range deployNode.DependsOn {
		if dep == notifyNode.ID {
			depsOnNotify = true
		}
		if dep == txnNode.ID {
			depsOnTxn = true
		}
	}
	if !depsOnNotify {
		t.Error("expected deployment to depend on smtp/notifications")
	}
	if depsOnTxn {
		t.Error("deployment should NOT depend on smtp/transactional (not referenced in env)")
	}
}
