package v1

import (
	"os"

	"github.com/davidthor/cldctl/pkg/schema/datacenter/internal"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// Transformer converts v1 schema to internal representation.
type Transformer struct{}

// NewTransformer creates a new v1 transformer.
func NewTransformer() *Transformer {
	return &Transformer{}
}

// Transform converts a v1 schema to the internal representation.
func (t *Transformer) Transform(v1 *SchemaV1) (*internal.InternalDatacenter, error) {
	dc := &internal.InternalDatacenter{
		SourceVersion: "v1",
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
		// Try to get the expression value to extract key-value pairs
		val, diags := c.VariablesExpr.Value(nil)
		if !diags.HasErrors() && (val.Type().IsObjectType() || val.Type().IsMapType()) {
			for k, v := range val.AsValueMap() {
				ic.Variables[k] = ctyValueToString(v)
			}
		} else {
			// Expression references runtime values - store individual attribute expressions
			// by reading from the source file
			rng := c.VariablesExpr.Range()
			if rng.Filename != "" {
				data, err := os.ReadFile(rng.Filename)
				if err == nil && rng.Start.Byte < len(data) && rng.End.Byte <= len(data) {
					// Store the entire expression as a single entry for runtime evaluation
					ic.Variables["__expr__"] = string(data[rng.Start.Byte:rng.End.Byte])
				}
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

	// Transform inputs - store as HCL expression strings
	if m.InputsExpr != nil {
		// Try to get the expression value to extract key-value pairs
		val, diags := m.InputsExpr.Value(nil)
		if !diags.HasErrors() && (val.Type().IsObjectType() || val.Type().IsMapType()) {
			for k, v := range val.AsValueMap() {
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

	return ie
}

func (t *Transformer) transformHooks(hooks []HookBlockV1) []internal.InternalHook {
	var result []internal.InternalHook

	for _, h := range hooks {
		ih := internal.InternalHook{
			When:    h.When,
			Error:   h.Error,
			Outputs: make(map[string]string),
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
					ih.Outputs[k] = ctyValueToString(v)
				}
			}
		} else if h.OutputsAttrs != nil {
			// Block syntax: outputs { ... }
			for name, attr := range h.OutputsAttrs {
				ih.Outputs[name] = exprToString(attr.Expr)
			}
		}

		result = append(result, ih)
	}

	return result
}

// exprToString converts an HCL expression to its string representation.
// For simple literals it returns the evaluated value. For complex expressions
// (e.g. module.postgres.url) it reads the original source text from the file.
func exprToString(expr hcl.Expression) string {
	// Try to evaluate as a simple value first (covers literals)
	val, diags := expr.Value(nil)
	if !diags.HasErrors() {
		return ctyValueToString(val)
	}

	// For complex expressions, read the original source text from the file
	rng := expr.Range()
	if rng.Filename != "" {
		data, err := os.ReadFile(rng.Filename)
		if err == nil && rng.Start.Byte < len(data) && rng.End.Byte <= len(data) {
			return string(data[rng.Start.Byte:rng.End.Byte])
		}
	}

	// Last resort: try to get source text from the expression's native range
	// (works for in-memory parsed expressions using hclsyntax)
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
