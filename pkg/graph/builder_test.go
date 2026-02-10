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

func TestBuilder_DatabaseUserNode_SingleDeployment(t *testing.T) {
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

func TestBuilder_NetworkPolicyNode_DeploymentReferencesService(t *testing.T) {
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
