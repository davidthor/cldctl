package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParser_ParseBytes_WithInstances(t *testing.T) {
	parser := NewParser()

	yaml := `
components:
  my-app:
    instances:
      - name: canary
        source: my-app:v2
        weight: 10
      - name: stable
        source: my-app:v1
        weight: 90
    distinct:
      - encryptionKey.signing
`

	schema, err := parser.ParseBytes([]byte(yaml))
	require.NoError(t, err)

	comp, ok := schema.Components["my-app"]
	require.True(t, ok)

	assert.Len(t, comp.Instances, 2)
	assert.Equal(t, "canary", comp.Instances[0].Name)
	assert.Equal(t, "my-app:v2", comp.Instances[0].Source)
	assert.Equal(t, 10, comp.Instances[0].Weight)
	assert.Equal(t, "stable", comp.Instances[1].Name)
	assert.Equal(t, "my-app:v1", comp.Instances[1].Source)
	assert.Equal(t, 90, comp.Instances[1].Weight)

	assert.Len(t, comp.Distinct, 1)
	assert.Equal(t, "encryptionKey.signing", comp.Distinct[0])
}

func TestParser_ParseBytes_WithInstanceVariables(t *testing.T) {
	parser := NewParser()

	yaml := `
components:
  my-app:
    instances:
      - name: canary
        source: my-app:v2
        weight: 10
        variables:
          feature_flag: "true"
      - name: stable
        source: my-app:v1
        weight: 90
`

	schema, err := parser.ParseBytes([]byte(yaml))
	require.NoError(t, err)

	comp := schema.Components["my-app"]
	assert.Len(t, comp.Instances, 2)
	assert.NotNil(t, comp.Instances[0].Variables)
	assert.Equal(t, "true", comp.Instances[0].Variables["feature_flag"])
	assert.Nil(t, comp.Instances[1].Variables)
}

func TestValidator_Validate_ValidInstances(t *testing.T) {
	validator := NewValidator()

	schema := &SchemaV1{
		Components: map[string]ComponentConfigV1{
			"my-app": {
				Instances: []InstanceConfigV1{
					{Name: "canary", Source: "my-app:v2", Weight: 10},
					{Name: "stable", Source: "my-app:v1", Weight: 90},
				},
			},
		},
	}

	errors := validator.Validate(schema)
	assert.Empty(t, errors)
}

func TestValidator_Validate_InstancesMissingName(t *testing.T) {
	validator := NewValidator()

	schema := &SchemaV1{
		Components: map[string]ComponentConfigV1{
			"my-app": {
				Instances: []InstanceConfigV1{
					{Name: "", Source: "my-app:v2", Weight: 10},
				},
			},
		},
	}

	errors := validator.Validate(schema)
	assert.NotEmpty(t, errors)
	assert.Contains(t, errors[0].Message, "instance name is required")
}

func TestValidator_Validate_InstancesMissingSource(t *testing.T) {
	validator := NewValidator()

	schema := &SchemaV1{
		Components: map[string]ComponentConfigV1{
			"my-app": {
				Instances: []InstanceConfigV1{
					{Name: "canary", Source: "", Weight: 10},
				},
			},
		},
	}

	errors := validator.Validate(schema)
	assert.NotEmpty(t, errors)
	assert.Contains(t, errors[0].Message, "instance source is required")
}

func TestValidator_Validate_InstancesDuplicateName(t *testing.T) {
	validator := NewValidator()

	schema := &SchemaV1{
		Components: map[string]ComponentConfigV1{
			"my-app": {
				Instances: []InstanceConfigV1{
					{Name: "canary", Source: "my-app:v2", Weight: 10},
					{Name: "canary", Source: "my-app:v3", Weight: 20},
				},
			},
		},
	}

	errors := validator.Validate(schema)
	assert.NotEmpty(t, errors)
	found := false
	for _, e := range errors {
		if e.Message == `duplicate instance name "canary"` {
			found = true
		}
	}
	assert.True(t, found, "expected duplicate name error")
}

func TestValidator_Validate_InstancesWeightExceeds100(t *testing.T) {
	validator := NewValidator()

	schema := &SchemaV1{
		Components: map[string]ComponentConfigV1{
			"my-app": {
				Instances: []InstanceConfigV1{
					{Name: "canary", Source: "my-app:v2", Weight: 60},
					{Name: "stable", Source: "my-app:v1", Weight: 50},
				},
			},
		},
	}

	errors := validator.Validate(schema)
	assert.NotEmpty(t, errors)
	found := false
	for _, e := range errors {
		if e.Field == "components.my-app.instances" {
			found = true
		}
	}
	assert.True(t, found, "expected total weight error")
}

func TestValidator_Validate_InstancesWeightOutOfRange(t *testing.T) {
	validator := NewValidator()

	schema := &SchemaV1{
		Components: map[string]ComponentConfigV1{
			"my-app": {
				Instances: []InstanceConfigV1{
					{Name: "canary", Source: "my-app:v2", Weight: -1},
				},
			},
		},
	}

	errors := validator.Validate(schema)
	assert.NotEmpty(t, errors)
	assert.Contains(t, errors[0].Message, "weight must be between 0 and 100")
}

func TestValidator_Validate_SingleInstanceNoPathOrImage(t *testing.T) {
	// Single-instance mode requires path or image
	validator := NewValidator()

	schema := &SchemaV1{
		Components: map[string]ComponentConfigV1{
			"my-app": {},
		},
	}

	errors := validator.Validate(schema)
	assert.NotEmpty(t, errors)
	assert.Contains(t, errors[0].Message, "either path or image is required")
}

func TestValidator_Validate_MultiInstanceNoTopLevelPathOrImage(t *testing.T) {
	// Multi-instance mode does NOT require top-level path/image
	validator := NewValidator()

	schema := &SchemaV1{
		Components: map[string]ComponentConfigV1{
			"my-app": {
				Instances: []InstanceConfigV1{
					{Name: "canary", Source: "my-app:v2", Weight: 100},
				},
			},
		},
	}

	errors := validator.Validate(schema)
	assert.Empty(t, errors)
}

func TestTransformer_Transform_WithInstances(t *testing.T) {
	transformer := NewTransformer()

	schema := &SchemaV1{
		Components: map[string]ComponentConfigV1{
			"my-app": {
				Instances: []InstanceConfigV1{
					{Name: "canary", Source: "my-app:v2", Weight: 10,
						Variables: map[string]interface{}{"flag": "true"}},
					{Name: "stable", Source: "my-app:v1", Weight: 90},
				},
				Distinct: []string{"encryptionKey.signing"},
			},
		},
	}

	env, err := transformer.Transform(schema)
	require.NoError(t, err)

	comp := env.Components["my-app"]
	assert.Len(t, comp.Instances, 2)
	assert.Equal(t, "canary", comp.Instances[0].Name)
	assert.Equal(t, "my-app:v2", comp.Instances[0].Source)
	assert.Equal(t, 10, comp.Instances[0].Weight)
	assert.Equal(t, "true", comp.Instances[0].Variables["flag"])
	assert.Equal(t, "stable", comp.Instances[1].Name)

	assert.Equal(t, []string{"encryptionKey.signing"}, comp.Distinct)
}
