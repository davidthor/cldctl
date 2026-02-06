package v1

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRuntimeV1_UnmarshalYAML_StringShorthand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLang string
	}{
		{
			name:     "node with major version",
			input:    `"node:20"`,
			wantLang: "node:20",
		},
		{
			name:     "python with semver",
			input:    `"python:^3.12"`,
			wantLang: "python:^3.12",
		},
		{
			name:     "go with exact version",
			input:    `"go:1.22"`,
			wantLang: "go:1.22",
		},
		{
			name:     "language without version",
			input:    `"node"`,
			wantLang: "node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rt RuntimeV1
			if err := yaml.Unmarshal([]byte(tt.input), &rt); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if rt.Language != tt.wantLang {
				t.Errorf("Language = %q, want %q", rt.Language, tt.wantLang)
			}
			if rt.OS != "" {
				t.Errorf("OS should be empty for string shorthand, got %q", rt.OS)
			}
			if rt.Arch != "" {
				t.Errorf("Arch should be empty for string shorthand, got %q", rt.Arch)
			}
			if len(rt.Packages) != 0 {
				t.Errorf("Packages should be empty for string shorthand, got %v", rt.Packages)
			}
			if len(rt.Setup) != 0 {
				t.Errorf("Setup should be empty for string shorthand, got %v", rt.Setup)
			}
		})
	}
}

func TestRuntimeV1_UnmarshalYAML_FullObject(t *testing.T) {
	input := `
language: node:20
os: linux
arch: arm64
packages:
  - ffmpeg
  - imagemagick
setup:
  - npm ci --production
`
	var rt RuntimeV1
	if err := yaml.Unmarshal([]byte(input), &rt); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if rt.Language != "node:20" {
		t.Errorf("Language = %q, want %q", rt.Language, "node:20")
	}
	if rt.OS != "linux" {
		t.Errorf("OS = %q, want %q", rt.OS, "linux")
	}
	if rt.Arch != "arm64" {
		t.Errorf("Arch = %q, want %q", rt.Arch, "arm64")
	}
	if len(rt.Packages) != 2 || rt.Packages[0] != "ffmpeg" || rt.Packages[1] != "imagemagick" {
		t.Errorf("Packages = %v, want [ffmpeg imagemagick]", rt.Packages)
	}
	if len(rt.Setup) != 1 || rt.Setup[0] != "npm ci --production" {
		t.Errorf("Setup = %v, want [npm ci --production]", rt.Setup)
	}
}

func TestRuntimeV1_UnmarshalYAML_LanguageOnly(t *testing.T) {
	input := `
language: python:^3.12
`
	var rt RuntimeV1
	if err := yaml.Unmarshal([]byte(input), &rt); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if rt.Language != "python:^3.12" {
		t.Errorf("Language = %q, want %q", rt.Language, "python:^3.12")
	}
	if rt.OS != "" {
		t.Errorf("OS should be empty, got %q", rt.OS)
	}
	if rt.Arch != "" {
		t.Errorf("Arch should be empty, got %q", rt.Arch)
	}
}

func TestRuntimeV1_UnmarshalYAML_InDeployment(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLang string
		wantOS   string
	}{
		{
			name: "string shorthand in deployment",
			input: `
image: ""
runtime: node:20
command: ["npm", "start"]
`,
			wantLang: "node:20",
			wantOS:   "",
		},
		{
			name: "full object in deployment",
			input: `
runtime:
  language: go:1.22
  os: linux
command: ["go", "run", "."]
`,
			wantLang: "go:1.22",
			wantOS:   "linux",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dep DeploymentV1
			if err := yaml.Unmarshal([]byte(tt.input), &dep); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if dep.Runtime == nil {
				t.Fatal("Runtime should not be nil")
			}
			if dep.Runtime.Language != tt.wantLang {
				t.Errorf("Runtime.Language = %q, want %q", dep.Runtime.Language, tt.wantLang)
			}
			if dep.Runtime.OS != tt.wantOS {
				t.Errorf("Runtime.OS = %q, want %q", dep.Runtime.OS, tt.wantOS)
			}
		})
	}
}

func TestRuntimeV1_UnmarshalYAML_DeploymentWithoutRuntime(t *testing.T) {
	input := `
image: nginx:latest
command: ["nginx", "-g", "daemon off;"]
`
	var dep DeploymentV1
	if err := yaml.Unmarshal([]byte(input), &dep); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if dep.Runtime != nil {
		t.Error("Runtime should be nil when not specified")
	}
	if dep.Image != "nginx:latest" {
		t.Errorf("Image = %q, want %q", dep.Image, "nginx:latest")
	}
}

func TestValidator_Validate_Runtime(t *testing.T) {
	validator := &Validator{}

	tests := []struct {
		name       string
		schema     *SchemaV1
		wantErrors int
	}{
		{
			name: "valid runtime string shorthand",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"worker": {
						Runtime: &RuntimeV1{Language: "node:20"},
						Command: []string{"node", "worker.js"},
					},
				},
			},
			wantErrors: 0,
		},
		{
			name: "valid runtime full object",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"worker": {
						Runtime: &RuntimeV1{
							Language: "python:^3.12",
							OS:       "linux",
							Arch:     "amd64",
							Packages: []string{"ffmpeg"},
							Setup:    []string{"pip install -r requirements.txt"},
						},
						Command: []string{"python", "worker.py"},
					},
				},
			},
			wantErrors: 0,
		},
		{
			name: "runtime missing language",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"worker": {
						Runtime: &RuntimeV1{
							OS: "linux",
						},
					},
				},
			},
			wantErrors: 1,
		},
		{
			name: "runtime invalid os",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"worker": {
						Runtime: &RuntimeV1{
							Language: "node:20",
							OS:       "macos",
						},
					},
				},
			},
			wantErrors: 1,
		},
		{
			name: "runtime invalid arch",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"worker": {
						Runtime: &RuntimeV1{
							Language: "node:20",
							Arch:     "x86",
						},
					},
				},
			},
			wantErrors: 1,
		},
		{
			name: "runtime with image (both allowed)",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"worker": {
						Image:   "myapp:latest",
						Runtime: &RuntimeV1{Language: "node:20"},
					},
				},
			},
			wantErrors: 0,
		},
		{
			name: "runtime invalid os and arch",
			schema: &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"worker": {
						Runtime: &RuntimeV1{
							Language: "node:20",
							OS:       "freebsd",
							Arch:     "mips",
						},
					},
				},
			},
			wantErrors: 2,
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

func TestTransformer_Transform_Runtime(t *testing.T) {
	transformer := NewTransformer()

	tests := []struct {
		name         string
		deployment   DeploymentV1
		wantRuntime  bool
		wantLanguage string
		wantOS       string
		wantArch     string
		wantPkgs     int
		wantSetup    int
	}{
		{
			name: "deployment without runtime",
			deployment: DeploymentV1{
				Image: "nginx:latest",
			},
			wantRuntime: false,
		},
		{
			name: "deployment with string shorthand runtime",
			deployment: DeploymentV1{
				Runtime: &RuntimeV1{Language: "node:20"},
				Command: []string{"node", "app.js"},
			},
			wantRuntime:  true,
			wantLanguage: "node:20",
		},
		{
			name: "deployment with full runtime",
			deployment: DeploymentV1{
				Runtime: &RuntimeV1{
					Language: "python:^3.12",
					OS:       "linux",
					Arch:     "arm64",
					Packages: []string{"ffmpeg", "imagemagick"},
					Setup:    []string{"pip install -r requirements.txt"},
				},
				Command: []string{"python", "app.py"},
			},
			wantRuntime:  true,
			wantLanguage: "python:^3.12",
			wantOS:       "linux",
			wantArch:     "arm64",
			wantPkgs:     2,
			wantSetup:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := &SchemaV1{
				Deployments: map[string]DeploymentV1{
					"test": tt.deployment,
				},
			}

			ic, err := transformer.Transform(schema)
			if err != nil {
				t.Fatalf("transform failed: %v", err)
			}

			if len(ic.Deployments) != 1 {
				t.Fatalf("expected 1 deployment, got %d", len(ic.Deployments))
			}

			dep := ic.Deployments[0]

			if tt.wantRuntime {
				if dep.Runtime == nil {
					t.Fatal("expected Runtime to be set")
				}
				if dep.Runtime.Language != tt.wantLanguage {
					t.Errorf("Language = %q, want %q", dep.Runtime.Language, tt.wantLanguage)
				}
				if dep.Runtime.OS != tt.wantOS {
					t.Errorf("OS = %q, want %q", dep.Runtime.OS, tt.wantOS)
				}
				if dep.Runtime.Arch != tt.wantArch {
					t.Errorf("Arch = %q, want %q", dep.Runtime.Arch, tt.wantArch)
				}
				if len(dep.Runtime.Packages) != tt.wantPkgs {
					t.Errorf("Packages count = %d, want %d", len(dep.Runtime.Packages), tt.wantPkgs)
				}
				if len(dep.Runtime.Setup) != tt.wantSetup {
					t.Errorf("Setup count = %d, want %d", len(dep.Runtime.Setup), tt.wantSetup)
				}
			} else {
				if dep.Runtime != nil {
					t.Error("expected Runtime to be nil")
				}
			}
		})
	}
}
