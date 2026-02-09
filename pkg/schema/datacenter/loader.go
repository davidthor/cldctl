package datacenter

import (
	"fmt"
	"os"

	"github.com/davidthor/cldctl/pkg/errors"
	"github.com/davidthor/cldctl/pkg/schema/datacenter/internal"
	"github.com/davidthor/cldctl/pkg/schema/datacenter/v1"
)

// versionDetectingLoader implements the Loader interface.
type versionDetectingLoader struct {
	parsers      map[string]*v1.Parser
	transformers map[string]*v1.Transformer
}

// NewLoader creates a new datacenter loader.
func NewLoader() Loader {
	return &versionDetectingLoader{
		parsers: map[string]*v1.Parser{
			"v1": v1.NewParser(),
		},
		transformers: map[string]*v1.Transformer{
			"v1": v1.NewTransformer(),
		},
	}
}

// Load parses a datacenter from the given path.
func (l *versionDetectingLoader) Load(path string) (Datacenter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(errors.ErrCodeParse, fmt.Sprintf("failed to read %s", path), err)
	}

	dc, err := l.LoadFromBytes(data, path)
	if err != nil {
		return nil, err
	}

	dc.Internal().SourcePath = path
	return dc, nil
}

// LoadFromBytes parses a datacenter from raw bytes.
func (l *versionDetectingLoader) LoadFromBytes(data []byte, sourcePath string) (Datacenter, error) {
	// Default to v1 parser
	parser := l.parsers["v1"]

	schema, diags, err := parser.ParseBytes(data, sourcePath)
	if err != nil {
		return nil, errors.ParseError(sourcePath, err)
	}

	if diags.HasErrors() {
		return nil, errors.ParseError(sourcePath, fmt.Errorf("%s", diags.Error()))
	}

	// Detect version (default to v1)
	version := schema.Version
	if version == "" {
		version = "v1"
	}

	// Transform to internal representation
	transformer, ok := l.transformers[version]
	if !ok {
		return nil, errors.New(errors.ErrCodeParse, fmt.Sprintf("unsupported schema version: %s", version))
	}

	internalDC, err := transformer.Transform(schema)
	if err != nil {
		return nil, errors.Wrap(errors.ErrCodeParse, "failed to transform schema", err)
	}

	internalDC.SourcePath = sourcePath

	return &datacenterWrapper{dc: internalDC}, nil
}

// Validate validates a datacenter without fully parsing.
func (l *versionDetectingLoader) Validate(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrap(errors.ErrCodeParse, fmt.Sprintf("failed to read %s", path), err)
	}

	parser := l.parsers["v1"]
	_, diags, err := parser.ParseBytes(data, path)
	if err != nil {
		return errors.ParseError(path, err)
	}

	if diags.HasErrors() {
		return errors.ParseError(path, fmt.Errorf("%s", diags.Error()))
	}

	return nil
}

// datacenterWrapper implements the Datacenter interface.
type datacenterWrapper struct {
	dc *internal.InternalDatacenter
}

func (d *datacenterWrapper) Variables() []Variable {
	result := make([]Variable, len(d.dc.Variables))
	for i := range d.dc.Variables {
		result[i] = &variableWrapper{v: &d.dc.Variables[i]}
	}
	return result
}

func (d *datacenterWrapper) Modules() []Module {
	result := make([]Module, len(d.dc.Modules))
	for i := range d.dc.Modules {
		result[i] = &moduleWrapper{m: &d.dc.Modules[i]}
	}
	return result
}

func (d *datacenterWrapper) Components() []DatacenterComponent {
	result := make([]DatacenterComponent, len(d.dc.Components))
	for i := range d.dc.Components {
		result[i] = &datacenterComponentWrapper{c: &d.dc.Components[i]}
	}
	return result
}

func (d *datacenterWrapper) Environment() Environment {
	return &environmentWrapper{e: &d.dc.Environment}
}

func (d *datacenterWrapper) SchemaVersion() string {
	return d.dc.SourceVersion
}

func (d *datacenterWrapper) SourcePath() string {
	return d.dc.SourcePath
}

func (d *datacenterWrapper) Internal() *internal.InternalDatacenter {
	return d.dc
}

// variableWrapper implements Variable interface.
type variableWrapper struct {
	v *internal.InternalVariable
}

func (v *variableWrapper) Name() string        { return v.v.Name }
func (v *variableWrapper) Type() string        { return v.v.Type }
func (v *variableWrapper) Description() string { return v.v.Description }
func (v *variableWrapper) Default() interface{} { return v.v.Default }
func (v *variableWrapper) Required() bool      { return v.v.Required }
func (v *variableWrapper) Sensitive() bool     { return v.v.Sensitive }

// datacenterComponentWrapper implements DatacenterComponent interface.
type datacenterComponentWrapper struct {
	c *internal.InternalDatacenterComponent
}

func (c *datacenterComponentWrapper) Name() string              { return c.c.Name }
func (c *datacenterComponentWrapper) Source() string            { return c.c.Source }
func (c *datacenterComponentWrapper) Variables() map[string]string { return c.c.Variables }

// moduleWrapper implements Module interface.
type moduleWrapper struct {
	m *internal.InternalModule
}

func (m *moduleWrapper) Name() string                  { return m.m.Name }
func (m *moduleWrapper) Build() string                 { return m.m.Build }
func (m *moduleWrapper) Source() string                { return m.m.Source }
func (m *moduleWrapper) Plugin() string                { return m.m.Plugin }
func (m *moduleWrapper) Inputs() map[string]string     { return m.m.Inputs }
func (m *moduleWrapper) Environment() map[string]string { return m.m.Environment }
func (m *moduleWrapper) When() string                  { return m.m.When }

func (m *moduleWrapper) Volumes() []VolumeMount {
	result := make([]VolumeMount, len(m.m.Volumes))
	for i := range m.m.Volumes {
		result[i] = &volumeMountWrapper{v: &m.m.Volumes[i]}
	}
	return result
}

// volumeMountWrapper implements VolumeMount interface.
type volumeMountWrapper struct {
	v *internal.InternalVolumeMount
}

func (v *volumeMountWrapper) HostPath() string  { return v.v.HostPath }
func (v *volumeMountWrapper) MountPath() string { return v.v.MountPath }
func (v *volumeMountWrapper) ReadOnly() bool    { return v.v.ReadOnly }

// environmentWrapper implements Environment interface.
type environmentWrapper struct {
	e *internal.InternalEnvironment
}

func (e *environmentWrapper) Modules() []Module {
	result := make([]Module, len(e.e.Modules))
	for i := range e.e.Modules {
		result[i] = &moduleWrapper{m: &e.e.Modules[i]}
	}
	return result
}

func (e *environmentWrapper) Hooks() Hooks {
	return &hooksWrapper{h: &e.e.Hooks}
}

// hooksWrapper implements Hooks interface.
type hooksWrapper struct {
	h *internal.InternalHooks
}

func (h *hooksWrapper) Database() []Hook { return wrapHooks(h.h.Database) }
func (h *hooksWrapper) Task() []Hook     { return wrapHooks(h.h.Task) }
func (h *hooksWrapper) Bucket() []Hook   { return wrapHooks(h.h.Bucket) }
func (h *hooksWrapper) EncryptionKey() []Hook     { return wrapHooks(h.h.EncryptionKey) }
func (h *hooksWrapper) SMTP() []Hook              { return wrapHooks(h.h.SMTP) }
func (h *hooksWrapper) DatabaseUser() []Hook      { return wrapHooks(h.h.DatabaseUser) }
func (h *hooksWrapper) Deployment() []Hook        { return wrapHooks(h.h.Deployment) }
func (h *hooksWrapper) Function() []Hook          { return wrapHooks(h.h.Function) }
func (h *hooksWrapper) Service() []Hook           { return wrapHooks(h.h.Service) }
func (h *hooksWrapper) Route() []Hook             { return wrapHooks(h.h.Route) }
func (h *hooksWrapper) Cronjob() []Hook           { return wrapHooks(h.h.Cronjob) }
func (h *hooksWrapper) Secret() []Hook            { return wrapHooks(h.h.Secret) }
func (h *hooksWrapper) DockerBuild() []Hook       { return wrapHooks(h.h.DockerBuild) }
func (h *hooksWrapper) Observability() []Hook     { return wrapHooks(h.h.Observability) }
func (h *hooksWrapper) Port() []Hook              { return wrapHooks(h.h.Port) }

func wrapHooks(hooks []internal.InternalHook) []Hook {
	result := make([]Hook, len(hooks))
	for i := range hooks {
		result[i] = &hookWrapper{h: &hooks[i]}
	}
	return result
}

// hookWrapper implements Hook interface.
type hookWrapper struct {
	h *internal.InternalHook
}

func (h *hookWrapper) When() string { return h.h.When }

func (h *hookWrapper) Modules() []Module {
	result := make([]Module, len(h.h.Modules))
	for i := range h.h.Modules {
		result[i] = &moduleWrapper{m: &h.h.Modules[i]}
	}
	return result
}

func (h *hookWrapper) Outputs() map[string]string { return h.h.Outputs }

func (h *hookWrapper) NestedOutputs() map[string]map[string]string { return h.h.NestedOutputs }

func (h *hookWrapper) Error() string { return h.h.Error }
