package v1

import (
	"testing"
)

func TestValidator_Validate(t *testing.T) {
	validator := &Validator{}

	tests := []struct {
		name       string
		schema     *SchemaV1
		wantErrors int
	}{
		{
			name:       "valid empty schema",
			schema:     &SchemaV1{},
			wantErrors: 0,
		},
		{
			name: "valid database",
			schema: &SchemaV1{
				Databases: map[string]DatabaseV1{
					"main": {Type: "postgres:^15"},
				},
			},
			wantErrors: 0,
		},
		{
			name: "database missing type",
			schema: &SchemaV1{
				Databases: map[string]DatabaseV1{
					"main": {Type: ""},
				},
			},
			wantErrors: 1,
		},
		{
			name: "invalid database type",
			schema: &SchemaV1{
				Databases: map[string]DatabaseV1{
					"main": {Type: "invalid"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "valid deployment with image",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"api": {Image: "nginx:latest"},
				},
			},
			wantErrors: 0,
		},
		{
			name: "valid deployment with build",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"api": {Build: &BuildV1{Context: "./api"}},
				},
			},
			wantErrors: 0,
		},
		{
			name: "deployment without image or build",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"api": {},
				},
			},
			wantErrors: 1,
		},
		{
			name: "service with deployment",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"api": {Image: "nginx:latest"},
				},
				Services: map[string]ServiceV1{
					"api": {Deployment: "api", Port: 8080},
				},
			},
			wantErrors: 0,
		},
		{
			name: "service without target",
			schema: &SchemaV1{
				Services: map[string]ServiceV1{
					"api": {Port: 8080},
				},
			},
			wantErrors: 1,
		},
		{
			name: "route with valid type and service",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"api": {Image: "nginx:latest"},
				},
				Services: map[string]ServiceV1{
					"api": {Deployment: "api", Port: 8080},
				},
				Routes: map[string]RouteV1{
					"main": {Type: "http", Service: "api"},
				},
			},
			wantErrors: 0,
		},
		{
			name: "route with invalid type",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"api": {Image: "nginx:latest"},
				},
				Services: map[string]ServiceV1{
					"api": {Deployment: "api", Port: 8080},
				},
				Routes: map[string]RouteV1{
					"main": {Type: "invalid", Service: "api"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "route with valid function",
			schema: &SchemaV1{
				Functions: map[string]FunctionV1{
					"web": {Build: &BuildV1{Context: "."}, Framework: "nextjs"},
				},
				Routes: map[string]RouteV1{
					"main": {Type: "http", Function: "web"},
				},
			},
			wantErrors: 0,
		},
		{
			name: "route with missing function",
			schema: &SchemaV1{
				Routes: map[string]RouteV1{
					"main": {Type: "http", Function: "missing"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "cronjob with valid schedule",
			schema: &SchemaV1{
				Cronjobs: map[string]CronjobV1{
					"cleanup": {
						Schedule: "0 * * * *",
						Image:    "alpine:latest",
					},
				},
			},
			wantErrors: 0,
		},
		{
			name: "cronjob without schedule",
			schema: &SchemaV1{
				Cronjobs: map[string]CronjobV1{
					"cleanup": {Image: "alpine:latest"},
				},
			},
			wantErrors: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := validator.Validate(tt.schema)
			if len(errors) != tt.wantErrors {
				t.Errorf("expected %d errors, got %d: %v", tt.wantErrors, len(errors), errors)
			}
		})
	}
}
