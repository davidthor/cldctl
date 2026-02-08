package environment

import (
	"fmt"
	"os"

	"github.com/davidthor/cldctl/pkg/errors"
	"github.com/davidthor/cldctl/pkg/schema/environment/internal"
	"github.com/davidthor/cldctl/pkg/schema/environment/v1"
)

// versionDetectingLoader implements the Loader interface.
type versionDetectingLoader struct {
	parsers      map[string]*v1.Parser
	validators   map[string]*v1.Validator
	transformers map[string]*v1.Transformer
}

// NewLoader creates a new environment loader.
func NewLoader() Loader {
	return &versionDetectingLoader{
		parsers: map[string]*v1.Parser{
			"v1": v1.NewParser(),
		},
		validators: map[string]*v1.Validator{
			"v1": v1.NewValidator(),
		},
		transformers: map[string]*v1.Transformer{
			"v1": v1.NewTransformer(),
		},
	}
}

// Load parses an environment from the given path.
func (l *versionDetectingLoader) Load(path string) (Environment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(errors.ErrCodeParse, fmt.Sprintf("failed to read %s", path), err)
	}

	env, err := l.LoadFromBytes(data, path)
	if err != nil {
		return nil, err
	}

	env.Internal().SourcePath = path
	return env, nil
}

// LoadFromBytes parses an environment from raw bytes.
func (l *versionDetectingLoader) LoadFromBytes(data []byte, sourcePath string) (Environment, error) {
	// Default to v1 parser
	parser := l.parsers["v1"]

	schema, err := parser.ParseBytes(data)
	if err != nil {
		return nil, errors.ParseError(sourcePath, err)
	}

	// Detect version (default to v1)
	version := schema.Version
	if version == "" {
		version = "v1"
	}

	// Validate
	validator, ok := l.validators[version]
	if !ok {
		return nil, errors.New(errors.ErrCodeParse, fmt.Sprintf("unsupported schema version: %s", version))
	}

	validationErrors := validator.Validate(schema)
	if len(validationErrors) > 0 {
		// Return first error
		return nil, errors.ValidationError(
			fmt.Sprintf("%s: %s", validationErrors[0].Field, validationErrors[0].Message),
			map[string]interface{}{"field": validationErrors[0].Field},
		)
	}

	// Transform to internal representation
	transformer, ok := l.transformers[version]
	if !ok {
		return nil, errors.New(errors.ErrCodeParse, fmt.Sprintf("unsupported schema version: %s", version))
	}

	internalEnv, err := transformer.Transform(schema)
	if err != nil {
		return nil, errors.Wrap(errors.ErrCodeParse, "failed to transform schema", err)
	}

	internalEnv.SourcePath = sourcePath

	return &environmentWrapper{env: internalEnv}, nil
}

// Validate validates an environment without fully parsing.
func (l *versionDetectingLoader) Validate(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrap(errors.ErrCodeParse, fmt.Sprintf("failed to read %s", path), err)
	}

	parser := l.parsers["v1"]
	schema, err := parser.ParseBytes(data)
	if err != nil {
		return errors.ParseError(path, err)
	}

	version := schema.Version
	if version == "" {
		version = "v1"
	}

	validator, ok := l.validators[version]
	if !ok {
		return errors.New(errors.ErrCodeParse, fmt.Sprintf("unsupported schema version: %s", version))
	}

	validationErrors := validator.Validate(schema)
	if len(validationErrors) > 0 {
		return errors.ValidationError(
			fmt.Sprintf("%s: %s", validationErrors[0].Field, validationErrors[0].Message),
			map[string]interface{}{"field": validationErrors[0].Field},
		)
	}

	return nil
}

// Transformer interface for transforming v1 schemas
type Transformer interface {
	Transform(schema *v1.SchemaV1) (*internal.InternalEnvironment, error)
}
