package importmap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseComponentMapping(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name: "valid mapping",
			content: `resources:
  database.main:
    - address: aws_db_instance.main
      id: "mydb-instance-123"
    - address: aws_security_group.db
      id: "sg-abc456"
  deployment.api:
    - address: aws_ecs_service.main
      id: "arn:aws:ecs:us-east-1:123:service/cluster/api"
`,
			wantErr: false,
		},
		{
			name:    "empty resources",
			content: `resources: {}`,
			wantErr: true,
		},
		{
			name: "missing address",
			content: `resources:
  database.main:
    - id: "mydb-123"
`,
			wantErr: true,
		},
		{
			name: "missing id",
			content: `resources:
  database.main:
    - address: aws_db_instance.main
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "mapping.yml")
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0644))

			mapping, err := ParseComponentMapping(path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, mapping)
				assert.Len(t, mapping.Resources["database.main"], 2)
				assert.Len(t, mapping.Resources["deployment.api"], 1)
			}
		})
	}
}

func TestParseEnvironmentMapping(t *testing.T) {
	content := `components:
  my-app:
    source: ghcr.io/myorg/app:v1.0.0
    variables:
      log_level: info
    resources:
      database.main:
        - address: aws_db_instance.main
          id: "mydb-instance-123"
      deployment.api:
        - address: aws_ecs_service.main
          id: "arn:aws:ecs:..."
  my-api:
    source: ghcr.io/myorg/api:v2.0.0
    resources:
      deployment.worker:
        - address: aws_ecs_service.main
          id: "arn:aws:ecs:..."
`

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "mapping.yml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	mapping, err := ParseEnvironmentMapping(path)
	require.NoError(t, err)
	assert.Len(t, mapping.Components, 2)
	assert.Equal(t, "ghcr.io/myorg/app:v1.0.0", mapping.Components["my-app"].Source)
	assert.Equal(t, "info", mapping.Components["my-app"].Variables["log_level"])
	assert.Len(t, mapping.Components["my-app"].Resources, 2)
}

func TestParseDatacenterMapping(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
		check   func(t *testing.T, m *DatacenterMapping)
	}{
		{
			name: "root modules only",
			content: `modules:
  alb:
    - address: aws_lb.this
      id: "arn:aws:elasticloadbalancing:..."
`,
			check: func(t *testing.T, m *DatacenterMapping) {
				assert.Len(t, m.Modules, 1)
				assert.Len(t, m.Modules["alb"], 1)
				assert.Empty(t, m.Environments)
			},
		},
		{
			name: "environments only",
			content: `environments:
  production:
    modules:
      vpc:
        - address: aws_vpc.this
          id: "vpc-0abc123"
`,
			check: func(t *testing.T, m *DatacenterMapping) {
				assert.Empty(t, m.Modules)
				assert.Len(t, m.Environments, 1)
				assert.Len(t, m.Environments["production"].Modules["vpc"], 1)
			},
		},
		{
			name: "root modules and environments",
			content: `modules:
  alb:
    - address: aws_lb.this
      id: "arn:..."

environments:
  production:
    modules:
      vpc:
        - address: aws_vpc.this
          id: "vpc-0abc123"
        - address: aws_subnet.public
          id: "subnet-aaa111"
  staging:
    modules:
      vpc:
        - address: aws_vpc.this
          id: "vpc-0def456"
`,
			check: func(t *testing.T, m *DatacenterMapping) {
				assert.Len(t, m.Modules, 1)
				assert.Len(t, m.Environments, 2)
				assert.Len(t, m.Environments["production"].Modules["vpc"], 2)
				assert.Len(t, m.Environments["staging"].Modules["vpc"], 1)
			},
		},
		{
			name:    "empty file",
			content: `{}`,
			wantErr: true,
		},
		{
			name: "environment with no modules",
			content: `environments:
  production:
    modules: {}
`,
			wantErr: true,
		},
		{
			name: "environment module missing address",
			content: `environments:
  production:
    modules:
      vpc:
        - id: "vpc-0abc123"
`,
			wantErr: true,
		},
		{
			name: "environment module missing id",
			content: `environments:
  production:
    modules:
      vpc:
        - address: aws_vpc.this
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "mapping.yml")
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0644))

			mapping, err := ParseDatacenterMapping(path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, mapping)
				tt.check(t, mapping)
			}
		})
	}
}

func TestParseMapFlags(t *testing.T) {
	tests := []struct {
		name    string
		flags   []string
		want    int
		wantErr bool
	}{
		{
			name:  "single mapping",
			flags: []string{"aws_db_instance.main=mydb-123"},
			want:  1,
		},
		{
			name: "multiple mappings",
			flags: []string{
				"aws_db_instance.main=mydb-123",
				"aws_security_group.db=sg-abc456",
			},
			want: 2,
		},
		{
			name:    "invalid format",
			flags:   []string{"no-equals-sign"},
			wantErr: true,
		},
		{
			name:    "empty id",
			flags:   []string{"aws_db_instance.main="},
			wantErr: true,
		},
		{
			name:  "id with equals sign",
			flags: []string{"aws_db_instance.main=arn:aws:rds:us-east-1:123:db/mydb=abc"},
			want:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mappings, err := ParseMapFlags(tt.flags)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, mappings, tt.want)
			}
		})
	}
}
