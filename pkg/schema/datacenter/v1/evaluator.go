package v1

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// Evaluator evaluates datacenter schemas with runtime context.
type Evaluator struct {
	ctx *EvalContext
}

// NewEvaluator creates a new evaluator.
func NewEvaluator() *Evaluator {
	return &Evaluator{
		ctx: NewEvalContext(),
	}
}

// WithContext sets the evaluation context.
func (e *Evaluator) WithContext(ctx *EvalContext) *Evaluator {
	e.ctx = ctx
	return e
}

// EvaluateWhen evaluates a when condition with the current context.
// Returns true if the condition passes or if there's no condition.
func (e *Evaluator) EvaluateWhen(expr hcl.Expression) (bool, error) {
	if expr == nil {
		return true, nil // No condition means always true
	}

	hclCtx := e.ctx.ToHCLContext()
	val, diags := expr.Value(hclCtx)
	if diags.HasErrors() {
		return false, fmt.Errorf("failed to evaluate when condition: %s", diags.Error())
	}

	// Handle both bool and string comparison results
	if val.Type() == cty.Bool {
		return val.True(), nil
	}

	// For string values, check if non-empty (truthy)
	if val.Type() == cty.String {
		return val.AsString() != "", nil
	}

	return !val.IsNull(), nil
}

// EvaluateInputs evaluates the inputs expression with the current context.
func (e *Evaluator) EvaluateInputs(expr hcl.Expression) (map[string]interface{}, error) {
	if expr == nil {
		return nil, nil
	}

	hclCtx := e.ctx.ToHCLContext()
	val, diags := expr.Value(hclCtx)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to evaluate inputs: %s", diags.Error())
	}

	if !val.Type().IsObjectType() && !val.Type().IsMapType() {
		return nil, fmt.Errorf("inputs must be an object or map, got %s", val.Type().FriendlyName())
	}

	result := make(map[string]interface{})
	for k, v := range val.AsValueMap() {
		result[k] = fromCtyValue(v)
	}

	return result, nil
}

// EvaluateOutputs evaluates the outputs expression with the current context.
func (e *Evaluator) EvaluateOutputs(expr hcl.Expression) (map[string]interface{}, error) {
	if expr == nil {
		return nil, nil
	}

	hclCtx := e.ctx.ToHCLContext()
	val, diags := expr.Value(hclCtx)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to evaluate outputs: %s", diags.Error())
	}

	if !val.Type().IsObjectType() && !val.Type().IsMapType() {
		return nil, fmt.Errorf("outputs must be an object or map, got %s", val.Type().FriendlyName())
	}

	result := make(map[string]interface{})
	for k, v := range val.AsValueMap() {
		result[k] = fromCtyValue(v)
	}

	return result, nil
}

// EvaluateModule evaluates a module block with the current context.
func (e *Evaluator) EvaluateModule(module *ModuleBlockV1) (*EvaluatedModule, error) {
	// Check when condition first
	shouldRun, err := e.EvaluateWhen(module.WhenExpr)
	if err != nil {
		return nil, fmt.Errorf("module %s: %w", module.Name, err)
	}

	if !shouldRun {
		return nil, nil // Module should be skipped
	}

	// Evaluate inputs
	inputs, err := e.EvaluateInputs(module.InputsExpr)
	if err != nil {
		return nil, fmt.Errorf("module %s inputs: %w", module.Name, err)
	}

	return &EvaluatedModule{
		Name:   module.Name,
		Build:  module.Build,
		Source: module.Source,
		Plugin: module.Plugin,
		Inputs: inputs,
	}, nil
}

// EvaluateHook evaluates a hook block and returns all modules that should run.
func (e *Evaluator) EvaluateHook(hook *HookBlockV1) (*EvaluatedHook, error) {
	// Check when condition
	shouldRun, err := e.EvaluateWhen(hook.WhenExpr)
	if err != nil {
		return nil, err
	}

	if !shouldRun {
		return nil, nil // Hook should be skipped
	}

	result := &EvaluatedHook{
		Modules: make([]*EvaluatedModule, 0),
	}

	// Evaluate each module
	for i := range hook.Modules {
		mod, err := e.EvaluateModule(&hook.Modules[i])
		if err != nil {
			return nil, err
		}
		if mod != nil {
			result.Modules = append(result.Modules, mod)
		}
	}

	// Store outputs for later evaluation (after modules complete)
	result.OutputsExpr = hook.OutputsExpr
	result.OutputsAttrs = hook.OutputsAttrs

	return result, nil
}

// FinalizeOutputs evaluates the outputs expression after modules have completed.
func (e *Evaluator) FinalizeOutputs(hook *EvaluatedHook) (map[string]interface{}, error) {
	if hook.OutputsExpr != nil {
		return e.EvaluateOutputs(hook.OutputsExpr)
	}
	if hook.OutputsAttrs != nil {
		return e.EvaluateOutputsFromAttrs(hook.OutputsAttrs)
	}
	return nil, nil
}

// EvaluateOutputsFromAttrs evaluates outputs from HCL attributes (block syntax).
func (e *Evaluator) EvaluateOutputsFromAttrs(attrs hcl.Attributes) (map[string]interface{}, error) {
	if attrs == nil {
		return nil, nil
	}

	hclCtx := e.ctx.ToHCLContext()
	result := make(map[string]interface{})

	for name, attr := range attrs {
		val, diags := attr.Expr.Value(hclCtx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("failed to evaluate output %s: %s", name, diags.Error())
		}
		result[name] = fromCtyValue(val)
	}

	return result, nil
}

// FindMatchingHook finds and evaluates the first matching hook from a list.
func (e *Evaluator) FindMatchingHook(hooks []HookBlockV1) (*EvaluatedHook, error) {
	for i := range hooks {
		hook, err := e.EvaluateHook(&hooks[i])
		if err != nil {
			return nil, err
		}
		if hook != nil {
			return hook, nil
		}
	}
	return nil, nil // No matching hook
}

// EvaluatedModule represents a module that has been evaluated and is ready to run.
type EvaluatedModule struct {
	Name   string
	Build  string
	Source string
	Plugin string
	Inputs map[string]interface{}
}

// EvaluatedHook represents a hook that has been evaluated and is ready to run.
type EvaluatedHook struct {
	Modules      []*EvaluatedModule
	OutputsExpr  hcl.Expression
	OutputsAttrs hcl.Attributes
}

// SetNodeContext sets the current node context for evaluation.
func (e *Evaluator) SetNodeContext(nodeType, name, component string, inputs map[string]interface{}) {
	nodeInputs := make(map[string]cty.Value)
	for k, v := range inputs {
		nodeInputs[k] = toCtyValue(v)
	}

	e.ctx.Node = &NodeContext{
		Type:      nodeType,
		Name:      name,
		Component: component,
		Inputs:    nodeInputs,
	}
}

// AddModuleOutputs adds outputs from a completed module to the context.
func (e *Evaluator) AddModuleOutputs(moduleName string, outputs map[string]interface{}) {
	e.ctx.WithModule(moduleName, outputs)
}

// SetEnvironmentContext sets the environment context.
func (e *Evaluator) SetEnvironmentContext(name, datacenter, account, region string) {
	e.ctx.Environment = &EnvironmentContext{
		Name:       name,
		Datacenter: datacenter,
		Account:    account,
		Region:     region,
	}
}

// SetVariables sets datacenter variables.
func (e *Evaluator) SetVariables(vars map[string]interface{}) {
	for k, v := range vars {
		e.ctx.WithVariable(k, v)
	}
}
