package v1

import (
	"fmt"
	"os"
	"strings"

	"github.com/davidthor/cldctl/pkg/schema/datacenter/internal"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Transformer converts v1 schema to internal representation.
type Transformer struct {
	// sourceBytes holds the raw HCL source that was parsed, allowing
	// exprToString to extract expression text using byte-offset ranges
	// even when the source was loaded from memory (not from a file on disk).
	sourceBytes []byte
}

// NewTransformer creates a new v1 transformer.
func NewTransformer() *Transformer {
	return &Transformer{}
}

// WithSourceBytes sets the raw source bytes for expression text extraction.
// Call this before Transform() when the source was loaded from memory.
func (t *Transformer) WithSourceBytes(data []byte) *Transformer {
	t.sourceBytes = data
	return t
}

// Transform converts a v1 schema to the internal representation.
func (t *Transformer) Transform(v1 *SchemaV1) (*internal.InternalDatacenter, error) {
	dc := &internal.InternalDatacenter{
		SourceVersion: "v1",
	}

	// Transform extends
	if v1.Extends != nil {
		dc.Extends = &internal.InternalExtends{
			Image: v1.Extends.Image,
			Path:  v1.Extends.Path,
		}
	}

	// Transform variables
	for _, v := range v1.Variables {
		iv := t.transformVariable(v)
		dc.Variables = append(dc.Variables, iv)
	}

	// Transform datacenter-level modules
	for _, m := range v1.Modules {
		im := t.transformModule(m)
		dc.Modules = append(dc.Modules, im)
	}

	// Transform datacenter-level components
	for _, c := range v1.Components {
		ic := t.transformComponent(c)
		dc.Components = append(dc.Components, ic)
	}

	// Transform environment
	if v1.Environment != nil {
		dc.Environment = t.transformEnvironment(v1.Environment)
	}

	// Validate that all hooks declare the required outputs. This catches
	// misconfigured hooks at build/validate time rather than at deploy time,
	// where missing outputs surface as cryptic unresolved expressions.
	if errs := t.validateHookOutputs(&dc.Environment.Hooks); len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return nil, fmt.Errorf("datacenter hook output validation failed:\n  - %s", strings.Join(msgs, "\n  - "))
	}

	return dc, nil
}

func (t *Transformer) transformVariable(v VariableBlockV1) internal.InternalVariable {
	iv := internal.InternalVariable{
		Name:        v.Name,
		Type:        v.Type,
		Description: v.Description,
		Sensitive:   v.Sensitive,
	}

	// Get default value if present
	if v.Default != nil {
		val, diags := v.Default.Expr.Value(nil)
		if !diags.HasErrors() {
			iv.Default = ctyValueToGo(val)
		}
	}

	// Determine if required (no default means required)
	iv.Required = v.Default == nil

	return iv
}

func (t *Transformer) transformComponent(c ComponentBlockV1) internal.InternalDatacenterComponent {
	ic := internal.InternalDatacenterComponent{
		Name:      c.Name,
		Source:    c.Source,
		Variables: make(map[string]string),
	}

	// Transform variables - store as HCL expression strings for runtime evaluation.
	// Variable values may reference datacenter variables (e.g., variable.stripe_key)
	// that are only known at deploy time.
	if c.VariablesExpr != nil {
		// Try to evaluate the expression directly (works for literal-only variables)
		val, diags := c.VariablesExpr.Value(nil)
		if !diags.HasErrors() && (val.Type().IsObjectType() || val.Type().IsMapType()) {
			for k, v := range val.AsValueMap() {
				ic.Variables[k] = ctyValueToString(v)
			}
		} else if objExpr, ok := c.VariablesExpr.(*hclsyntax.ObjectConsExpr); ok {
			// Direct evaluation failed (e.g., variables contain variable references).
			// Walk the AST to extract each key-value pair as expression strings.
			for _, item := range objExpr.Items {
				key, keyDiags := item.KeyExpr.Value(nil)
				if keyDiags.HasErrors() {
					continue
				}
				ic.Variables[key.AsString()] = exprToString(item.ValueExpr, t.sourceBytes)
			}
		}
	}

	return ic
}

func (t *Transformer) transformModule(m ModuleBlockV1) internal.InternalModule {
	im := internal.InternalModule{
		Name:   m.Name,
		Build:  m.Build,
		Source: m.Source,
		Plugin: m.Plugin,
		When:   m.When,
		Inputs: make(map[string]string),
	}

	// Default plugin to pulumi
	if im.Plugin == "" {
		im.Plugin = "pulumi"
	}

	// Transform inputs - store as HCL expression strings so the engine can
	// evaluate them at deploy time with actual variable values.
	if m.InputsExpr != nil {
		// Try to evaluate the expression directly (works for literal-only inputs)
		val, diags := m.InputsExpr.Value(nil)
		if !diags.HasErrors() && (val.Type().IsObjectType() || val.Type().IsMapType()) {
			for k, v := range val.AsValueMap() {
				im.Inputs[k] = ctyValueToString(v)
			}
		} else if objExpr, ok := m.InputsExpr.(*hclsyntax.ObjectConsExpr); ok {
			// Direct evaluation failed (e.g., inputs contain variable references).
			// Walk the AST to extract each key-value pair as expression strings
			// so they can be resolved at deploy time.
			for _, item := range objExpr.Items {
				key, keyDiags := item.KeyExpr.Value(nil)
				if keyDiags.HasErrors() {
					continue
				}
				im.Inputs[key.AsString()] = exprToString(item.ValueExpr, t.sourceBytes)
			}
		} else if m.InputsEvaluated != nil {
			// Fallback: use values that were evaluated during parsing with context
			for k, v := range m.InputsEvaluated {
				im.Inputs[k] = ctyValueToString(v)
			}
		}
	} else if m.InputsEvaluated != nil {
		for k, v := range m.InputsEvaluated {
			im.Inputs[k] = ctyValueToString(v)
		}
	}

	// Transform volumes
	for _, vol := range m.Volumes {
		im.Volumes = append(im.Volumes, internal.InternalVolumeMount{
			HostPath:  vol.HostPath,
			MountPath: vol.MountPath,
			ReadOnly:  vol.ReadOnly,
		})
	}

	return im
}

func (t *Transformer) transformEnvironment(env *EnvironmentBlockV1) internal.InternalEnvironment {
	ie := internal.InternalEnvironment{}

	// Transform modules
	for _, m := range env.Modules {
		ie.Modules = append(ie.Modules, t.transformModule(m))
	}

	// Transform hooks
	ie.Hooks.Database = t.transformHooks(env.DatabaseHooks)
	ie.Hooks.Task = t.transformHooks(env.TaskHooks)
	ie.Hooks.Bucket = t.transformHooks(env.BucketHooks)
	ie.Hooks.EncryptionKey = t.transformHooks(env.EncryptionKeyHooks)
	ie.Hooks.SMTP = t.transformHooks(env.SMTPHooks)
	ie.Hooks.DatabaseUser = t.transformHooks(env.DatabaseUserHooks)
	ie.Hooks.Deployment = t.transformHooks(env.DeploymentHooks)
	ie.Hooks.Function = t.transformHooks(env.FunctionHooks)
	ie.Hooks.Service = t.transformHooks(env.ServiceHooks)
	ie.Hooks.Route = t.transformHooks(env.RouteHooks)
	ie.Hooks.Cronjob = t.transformHooks(env.CronjobHooks)
	ie.Hooks.Secret = t.transformHooks(env.SecretHooks)
	ie.Hooks.DockerBuild = t.transformHooks(env.DockerBuildHooks)
	ie.Hooks.Observability = t.transformHooks(env.ObservabilityHooks)
	ie.Hooks.Port = t.transformHooks(env.PortHooks)
	ie.Hooks.NetworkPolicy = t.transformHooks(env.NetworkPolicyHooks)

	return ie
}

func (t *Transformer) transformHooks(hooks []HookBlockV1) []internal.InternalHook {
	var result []internal.InternalHook

	for _, h := range hooks {
		when := h.When
		// If the when condition couldn't be evaluated at parse time (e.g., it
		// references node.inputs which are only available at deploy time), fall
		// back to the raw expression source text so the executor can evaluate it.
		if when == "" && h.WhenExpr != nil {
			when = exprToString(h.WhenExpr, t.sourceBytes)
		}

		ih := internal.InternalHook{
			When:          when,
			Error:         h.Error,
			Outputs:       make(map[string]string),
			NestedOutputs: make(map[string]map[string]string),
		}

		// Transform modules
		for _, m := range h.Modules {
			ih.Modules = append(ih.Modules, t.transformModule(m))
		}

		// Transform outputs - can be from expression or attributes
		if h.OutputsExpr != nil {
			// Attribute syntax: outputs = { ... }
			val, diags := h.OutputsExpr.Value(nil)
			if !diags.HasErrors() && (val.Type().IsObjectType() || val.Type().IsMapType()) {
				for k, v := range val.AsValueMap() {
					// Check if value is a nested object (e.g., read = { ... }, write = { ... })
					if (v.Type().IsObjectType() || v.Type().IsMapType()) && !v.IsNull() {
						nested := make(map[string]string)
						for nk, nv := range v.AsValueMap() {
							nested[nk] = ctyValueToString(nv)
						}
						ih.NestedOutputs[k] = nested
					} else {
						ih.Outputs[k] = ctyValueToString(v)
					}
				}
			} else if objExpr, ok := h.OutputsExpr.(*hclsyntax.ObjectConsExpr); ok {
				// Evaluation failed (e.g., expressions reference module outputs not
				// available at parse time). Fall back to extracting individual
				// key-value expression strings from the object literal so they can
				// be resolved at runtime.
				for _, item := range objExpr.Items {
					keyVal, keyDiags := item.KeyExpr.Value(nil)
					if keyDiags.HasErrors() {
						continue
					}
					key := keyVal.AsString()

					// Check if the value is a nested object (e.g., read = { ... })
					if nested, ok := item.ValueExpr.(*hclsyntax.ObjectConsExpr); ok {
						nestedMap := make(map[string]string)
						for _, ni := range nested.Items {
							nkVal, nkDiags := ni.KeyExpr.Value(nil)
							if nkDiags.HasErrors() {
								continue
							}
							nestedMap[nkVal.AsString()] = exprToString(ni.ValueExpr, t.sourceBytes)
						}
						if len(nestedMap) > 0 {
							ih.NestedOutputs[key] = nestedMap
						}
					} else {
						ih.Outputs[key] = exprToString(item.ValueExpr, t.sourceBytes)
					}
				}
			}
		} else if h.OutputsAttrs != nil {
			// Block syntax: outputs { ... }
			for name, attr := range h.OutputsAttrs {
				ih.Outputs[name] = exprToString(attr.Expr, t.sourceBytes)
			}
		}

		// Transform nested output expressions (from block syntax parsing)
		for name, expr := range h.NestedOutputExprs {
			val, diags := expr.Value(nil)
			if !diags.HasErrors() && (val.Type().IsObjectType() || val.Type().IsMapType()) {
				nested := make(map[string]string)
				for nk, nv := range val.AsValueMap() {
					nested[nk] = ctyValueToString(nv)
				}
				ih.NestedOutputs[name] = nested
			} else {
				// Store as expression strings for runtime evaluation
				nested := make(map[string]string)
				if objExpr, ok := expr.(*hclsyntax.ObjectConsExpr); ok {
					for _, item := range objExpr.Items {
						key, keyDiags := item.KeyExpr.Value(nil)
						if keyDiags.HasErrors() {
							continue
						}
						nested[key.AsString()] = exprToString(item.ValueExpr, t.sourceBytes)
					}
				}
				if len(nested) > 0 {
					ih.NestedOutputs[name] = nested
				}
			}
		}

		result = append(result, ih)
	}

	return result
}

// exprToString converts an HCL expression to its string representation.
// For simple literals it returns the evaluated value. For complex expressions
// (e.g. module.postgres.url) it reads the original source text from the file.
// An optional sourceBytes parameter allows extraction when the source was loaded
// from memory (e.g. LoadFromBytes) and the file doesn't exist on disk.
func exprToString(expr hcl.Expression, sourceBytes ...[]byte) string {
	// Try to evaluate as a simple value first (covers literals)
	val, diags := expr.Value(nil)
	if !diags.HasErrors() {
		return ctyValueToString(val)
	}

	rng := expr.Range()

	// Try in-memory source bytes first (available when loaded via LoadFromBytes)
	if len(sourceBytes) > 0 && sourceBytes[0] != nil {
		data := sourceBytes[0]
		if rng.Start.Byte < len(data) && rng.End.Byte <= len(data) {
			source := string(data[rng.Start.Byte:rng.End.Byte])
			if len(source) >= 2 && source[0] == '"' && source[len(source)-1] == '"' {
				source = source[1 : len(source)-1]
			}
			return source
		}
	}

	// For complex expressions, read the original source text from the file
	if rng.Filename != "" {
		data, err := os.ReadFile(rng.Filename)
		if err == nil && rng.Start.Byte < len(data) && rng.End.Byte <= len(data) {
			source := string(data[rng.Start.Byte:rng.End.Byte])
			// Strip surrounding HCL string quotes – the range for a string template
			// expression like "${env}-${name}" includes the quote delimiters, but
			// the semantic value should not contain them.
			if len(source) >= 2 && source[0] == '"' && source[len(source)-1] == '"' {
				source = source[1 : len(source)-1]
			}
			return source
		}
	}

	// Last resort
	return "<expression>"
}

// ctyValueToString converts a cty.Value to its string representation.
func ctyValueToString(val cty.Value) string {
	if val.IsNull() {
		return ""
	}
	switch {
	case val.Type() == cty.String:
		return val.AsString()
	case val.Type() == cty.Number:
		return val.AsBigFloat().String()
	case val.Type() == cty.Bool:
		if val.True() {
			return "true"
		}
		return "false"
	default:
		// For complex types, return the Go string representation
		return val.GoString()
	}
}

// ctyValueToGo converts a cty.Value to a Go interface{}.
func ctyValueToGo(val cty.Value) interface{} {
	if val.IsNull() {
		return nil
	}

	switch {
	case val.Type() == cty.String:
		return val.AsString()
	case val.Type() == cty.Number:
		bf := val.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return i
		}
		f, _ := bf.Float64()
		return f
	case val.Type() == cty.Bool:
		return val.True()
	case val.Type().IsListType() || val.Type().IsTupleType():
		var result []interface{}
		for it := val.ElementIterator(); it.Next(); {
			_, v := it.Element()
			result = append(result, ctyValueToGo(v))
		}
		return result
	case val.Type().IsMapType() || val.Type().IsObjectType():
		result := make(map[string]interface{})
		for it := val.ElementIterator(); it.Next(); {
			k, v := it.Element()
			result[k.AsString()] = ctyValueToGo(v)
		}
		return result
	default:
		return nil
	}
}

// validateHookOutputs validates that each hook type in the environment block
// declares all required outputs. Returns any validation errors found.
func (t *Transformer) validateHookOutputs(hooks *internal.InternalHooks) []error {
	var errs []error

	hookMap := map[string][]internal.InternalHook{
		"database":      hooks.Database,
		"task":          hooks.Task,
		"bucket":        hooks.Bucket,
		"encryptionKey": hooks.EncryptionKey,
		"smtp":          hooks.SMTP,
		"deployment":    hooks.Deployment,
		"function":      hooks.Function,
		"service":       hooks.Service,
		"route":         hooks.Route,
		"cronjob":       hooks.Cronjob,
		"secret":        hooks.Secret,
		"dockerBuild":   hooks.DockerBuild,
		"observability": hooks.Observability,
		"port":          hooks.Port,
		"databaseUser":  hooks.DatabaseUser,
		"networkPolicy": hooks.NetworkPolicy,
	}

	for hookType, hookList := range hookMap {
		if hookErrs := internal.ValidateHookOutputs(hookType, hookList); len(hookErrs) > 0 {
			errs = append(errs, hookErrs...)
		}
	}

	return errs
}
