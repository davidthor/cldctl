package v1

import (
	"fmt"
	"os"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

// Parser parses v1 datacenter schemas.
type Parser struct {
	parser *hclparse.Parser
	// evalCtx holds the evaluation context for HCL expressions
	evalCtx *EvalContext
}

// NewParser creates a new v1 parser.
func NewParser() *Parser {
	return &Parser{
		parser:  hclparse.NewParser(),
		evalCtx: NewEvalContext(),
	}
}

// WithContext sets the evaluation context for the parser.
func (p *Parser) WithContext(ctx *EvalContext) *Parser {
	p.evalCtx = ctx
	return p
}

// WithVariable adds a variable to the evaluation context.
func (p *Parser) WithVariable(name string, value interface{}) *Parser {
	p.evalCtx.WithVariable(name, value)
	return p
}

// WithEnvironment sets the environment context.
func (p *Parser) WithEnvironment(env *EnvironmentContext) *Parser {
	p.evalCtx.WithEnvironment(env)
	return p
}

// getHCLContext returns the HCL evaluation context.
func (p *Parser) getHCLContext() *hcl.EvalContext {
	return p.evalCtx.ToHCLContext()
}

// Parse parses a datacenter from the given file path.
func (p *Parser) Parse(path string) (*SchemaV1, hcl.Diagnostics, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file: %w", err)
	}
	return p.ParseBytes(data, path)
}

// ParseBytes parses a datacenter from raw bytes.
func (p *Parser) ParseBytes(data []byte, filename string) (*SchemaV1, hcl.Diagnostics, error) {
	file, diags := p.parser.ParseHCL(data, filename)
	if diags.HasErrors() {
		return nil, diags, fmt.Errorf("failed to parse HCL: %s", diags.Error())
	}

	schema := &SchemaV1{}

	// Define the schema for decoding
	bodySchema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "version"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "variable", LabelNames: []string{"name"}},
			{Type: "module", LabelNames: []string{"name"}},
			{Type: "component", LabelNames: []string{"name"}},
			{Type: "environment"},
		},
	}

	content, moreDiags := file.Body.Content(bodySchema)
	diags = append(diags, moreDiags...)

	// Get HCL evaluation context
	hclCtx := p.getHCLContext()

	// Parse version
	if attr, ok := content.Attributes["version"]; ok {
		val, valDiags := attr.Expr.Value(hclCtx)
		diags = append(diags, valDiags...)
		if !valDiags.HasErrors() {
			schema.Version = val.AsString()
		}
	}

	// Parse variables
	for _, block := range content.Blocks.OfType("variable") {
		variable, blockDiags := p.parseVariable(block)
		diags = append(diags, blockDiags...)
		if variable != nil {
			schema.Variables = append(schema.Variables, *variable)
		}
	}

	// Parse top-level modules
	for _, block := range content.Blocks.OfType("module") {
		module, blockDiags := p.parseModule(block)
		diags = append(diags, blockDiags...)
		if module != nil {
			schema.Modules = append(schema.Modules, *module)
		}
	}

	// Parse top-level component declarations
	for _, block := range content.Blocks.OfType("component") {
		comp, blockDiags := p.parseComponent(block)
		diags = append(diags, blockDiags...)
		if comp != nil {
			schema.Components = append(schema.Components, *comp)
		}
	}

	// Parse environment block
	for _, block := range content.Blocks.OfType("environment") {
		env, blockDiags := p.parseEnvironment(block)
		diags = append(diags, blockDiags...)
		schema.Environment = env
		break // Only one environment block allowed
	}

	return schema, diags, nil
}

func (p *Parser) parseVariable(block *hcl.Block) (*VariableBlockV1, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	hclCtx := p.getHCLContext()

	varSchema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "type"},
			{Name: "description"},
			{Name: "default"},
			{Name: "sensitive"},
		},
	}

	content, moreDiags := block.Body.Content(varSchema)
	diags = append(diags, moreDiags...)

	variable := &VariableBlockV1{
		Name: block.Labels[0],
	}

	if attr, ok := content.Attributes["type"]; ok {
		// Type is a type constraint (string, number, bool, list, map, etc.)
		// not an expression to evaluate. Extract it as a keyword.
		variable.Type = hcl.ExprAsKeyword(attr.Expr)
	}

	if attr, ok := content.Attributes["description"]; ok {
		val, valDiags := attr.Expr.Value(hclCtx)
		diags = append(diags, valDiags...)
		if !valDiags.HasErrors() {
			variable.Description = val.AsString()
		}
	}

	if attr, ok := content.Attributes["default"]; ok {
		variable.Default = attr
		// Also evaluate and store the default value for use in context
		val, valDiags := attr.Expr.Value(hclCtx)
		if !valDiags.HasErrors() {
			variable.DefaultValue = val
			// Add to evaluation context for subsequent references
			p.evalCtx.WithVariable(variable.Name, fromCtyValue(val))
		}
	}

	if attr, ok := content.Attributes["sensitive"]; ok {
		val, valDiags := attr.Expr.Value(hclCtx)
		diags = append(diags, valDiags...)
		if !valDiags.HasErrors() {
			variable.Sensitive = val.True()
		}
	}

	return variable, diags
}

func (p *Parser) parseModule(block *hcl.Block) (*ModuleBlockV1, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	hclCtx := p.getHCLContext()

	moduleSchema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "build"},
			{Name: "source"},
			{Name: "plugin"},
			{Name: "when"},
			{Name: "environment"},
			{Name: "inputs"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "inputs"},
			{Type: "volume"},
		},
	}

	content, moreDiags := block.Body.Content(moduleSchema)
	diags = append(diags, moreDiags...)

	module := &ModuleBlockV1{
		Name:   block.Labels[0],
		Remain: block.Body,
	}

	if attr, ok := content.Attributes["build"]; ok {
		val, valDiags := attr.Expr.Value(hclCtx)
		diags = append(diags, valDiags...)
		if !valDiags.HasErrors() {
			module.Build = val.AsString()
		}
	}

	if attr, ok := content.Attributes["source"]; ok {
		val, valDiags := attr.Expr.Value(hclCtx)
		diags = append(diags, valDiags...)
		if !valDiags.HasErrors() {
			module.Source = val.AsString()
		}
	}

	if attr, ok := content.Attributes["plugin"]; ok {
		val, valDiags := attr.Expr.Value(hclCtx)
		diags = append(diags, valDiags...)
		if !valDiags.HasErrors() {
			module.Plugin = val.AsString()
		}
	}

	if attr, ok := content.Attributes["when"]; ok {
		// Store the raw expression for when - it may contain node.inputs references
		// that need to be evaluated at runtime
		module.WhenExpr = attr.Expr
		// Try to evaluate if possible
		val, valDiags := attr.Expr.Value(hclCtx)
		if !valDiags.HasErrors() {
			// When can be a boolean or string expression
			switch val.Type() {
			case cty.Bool:
				if val.True() {
					module.When = "true"
				} else {
					module.When = "false"
				}
			case cty.String:
				module.When = val.AsString()
			}
		}
	}

	// Parse inputs - can be either an attribute (inputs = {...}) or a block (inputs {...})
	if attr, ok := content.Attributes["inputs"]; ok {
		// Attribute syntax: inputs = { ... }
		module.InputsExpr = attr.Expr
		// Try to evaluate the inputs if context is available
		val, valDiags := attr.Expr.Value(hclCtx)
		if !valDiags.HasErrors() && val.Type().IsObjectType() {
			module.InputsEvaluated = val.AsValueMap()
		}
	} else if inputsBlocks := content.Blocks.OfType("inputs"); len(inputsBlocks) > 0 {
		// Block syntax: inputs { ... } - only process first block
		inputsBlock := inputsBlocks[0]
		attrs, attrDiags := inputsBlock.Body.JustAttributes()
		diags = append(diags, attrDiags...)
		if len(attrs) > 0 {
			module.InputsEvaluated = make(map[string]cty.Value)
			for name, attr := range attrs {
				val, valDiags := attr.Expr.Value(hclCtx)
				if !valDiags.HasErrors() {
					module.InputsEvaluated[name] = val
				}
			}
		}
	}

	// Parse volume blocks
	for _, volBlock := range content.Blocks.OfType("volume") {
		vol, volDiags := p.parseVolume(volBlock)
		diags = append(diags, volDiags...)
		if vol != nil {
			module.Volumes = append(module.Volumes, *vol)
		}
	}

	return module, diags
}

func (p *Parser) parseVolume(block *hcl.Block) (*VolumeBlockV1, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	hclCtx := p.getHCLContext()

	volSchema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "host_path", Required: true},
			{Name: "mount_path", Required: true},
			{Name: "read_only"},
		},
	}

	content, moreDiags := block.Body.Content(volSchema)
	diags = append(diags, moreDiags...)

	vol := &VolumeBlockV1{}

	if attr, ok := content.Attributes["host_path"]; ok {
		val, valDiags := attr.Expr.Value(hclCtx)
		diags = append(diags, valDiags...)
		if !valDiags.HasErrors() {
			vol.HostPath = val.AsString()
		}
	}

	if attr, ok := content.Attributes["mount_path"]; ok {
		val, valDiags := attr.Expr.Value(hclCtx)
		diags = append(diags, valDiags...)
		if !valDiags.HasErrors() {
			vol.MountPath = val.AsString()
		}
	}

	if attr, ok := content.Attributes["read_only"]; ok {
		val, valDiags := attr.Expr.Value(hclCtx)
		diags = append(diags, valDiags...)
		if !valDiags.HasErrors() {
			vol.ReadOnly = val.True()
		}
	}

	return vol, diags
}

func (p *Parser) parseEnvironment(block *hcl.Block) (*EnvironmentBlockV1, hcl.Diagnostics) {
	var diags hcl.Diagnostics

	envSchema := &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "module", LabelNames: []string{"name"}},
			{Type: "database"},
			{Type: "task"},
			{Type: "bucket"},
			{Type: "encryptionKey"},
			{Type: "smtp"},
			{Type: "databaseUser"},
			{Type: "deployment"},
			{Type: "function"},
			{Type: "service"},
			{Type: "route"},
			{Type: "cronjob"},
			{Type: "secret"},
			{Type: "dockerBuild"},
			{Type: "observability"},
			{Type: "port"},
		},
	}

	content, moreDiags := block.Body.Content(envSchema)
	diags = append(diags, moreDiags...)

	env := &EnvironmentBlockV1{
		Remain: block.Body,
	}

	// Parse modules
	for _, modBlock := range content.Blocks.OfType("module") {
		module, modDiags := p.parseModule(modBlock)
		diags = append(diags, modDiags...)
		if module != nil {
			env.Modules = append(env.Modules, *module)
		}
	}

	// Parse hooks
	hookTypes := map[string]*[]HookBlockV1{
		"database":          &env.DatabaseHooks,
		"task":              &env.TaskHooks,
		"bucket":            &env.BucketHooks,
		"encryptionKey":     &env.EncryptionKeyHooks,
		"smtp":              &env.SMTPHooks,
		"databaseUser":      &env.DatabaseUserHooks,
		"deployment":        &env.DeploymentHooks,
		"function":          &env.FunctionHooks,
		"service":           &env.ServiceHooks,
		"route":             &env.RouteHooks,
		"cronjob":           &env.CronjobHooks,
		"secret":            &env.SecretHooks,
		"dockerBuild":       &env.DockerBuildHooks,
		"observability":     &env.ObservabilityHooks,
		"port":              &env.PortHooks,
	}

	for hookType, hooks := range hookTypes {
		for _, hookBlock := range content.Blocks.OfType(hookType) {
			hook, hookDiags := p.parseHook(hookBlock)
			diags = append(diags, hookDiags...)
			if hook != nil {
				*hooks = append(*hooks, *hook)
			}
		}

		// Validate reachability: a hook without a 'when' condition (catch-all)
		// must be the last hook of its type. Any hooks after it are unreachable.
		for i, hook := range *hooks {
			if hook.When == "" && hook.WhenExpr == nil && i < len(*hooks)-1 {
				remaining := len(*hooks) - i - 1
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  fmt.Sprintf("Unreachable %s hook(s)", hookType),
					Detail:   fmt.Sprintf("A %s hook without a 'when' condition matches all resources and must be the last hook of its type, but %d more %s hook(s) follow. Move the catch-all hook to the end or add a 'when' condition.", hookType, remaining, hookType),
				})
				break
			}
		}
	}

	return env, diags
}

func (p *Parser) parseComponent(block *hcl.Block) (*ComponentBlockV1, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	hclCtx := p.getHCLContext()

	compSchema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "source", Required: true},
			{Name: "variables"},
		},
	}

	content, moreDiags := block.Body.Content(compSchema)
	diags = append(diags, moreDiags...)

	comp := &ComponentBlockV1{
		Name:   block.Labels[0],
		Remain: block.Body,
	}

	if attr, ok := content.Attributes["source"]; ok {
		val, valDiags := attr.Expr.Value(hclCtx)
		diags = append(diags, valDiags...)
		if !valDiags.HasErrors() {
			comp.Source = val.AsString()
		}
	}

	if attr, ok := content.Attributes["variables"]; ok {
		// Store the raw expression for runtime evaluation (variables may reference
		// datacenter variables that are only known at deploy time)
		comp.VariablesExpr = attr.Expr
	}

	return comp, diags
}

func (p *Parser) parseHook(block *hcl.Block) (*HookBlockV1, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	hclCtx := p.getHCLContext()

	hookSchema := &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "when"},
			{Name: "outputs"},
			{Name: "error"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "module", LabelNames: []string{"name"}},
			{Type: "outputs"},
		},
	}

	content, moreDiags := block.Body.Content(hookSchema)
	diags = append(diags, moreDiags...)

	hook := &HookBlockV1{
		Remain: block.Body,
	}

	if attr, ok := content.Attributes["when"]; ok {
		// Store raw expression for runtime evaluation
		hook.WhenExpr = attr.Expr
		// Try to evaluate if possible (may fail if references node.inputs)
		val, valDiags := attr.Expr.Value(hclCtx)
		if !valDiags.HasErrors() {
			// When can be a boolean or string expression
			switch val.Type() {
			case cty.Bool:
				if val.True() {
					hook.When = "true"
				} else {
					hook.When = "false"
				}
			case cty.String:
				hook.When = val.AsString()
			}
		}
	}

	// Parse error attribute
	if attr, ok := content.Attributes["error"]; ok {
		hook.ErrorExpr = attr.Expr
		// Try to evaluate if possible (may fail if contains interpolations like ${node.inputs.type})
		val, valDiags := attr.Expr.Value(hclCtx)
		if !valDiags.HasErrors() && val.Type() == cty.String {
			hook.Error = val.AsString()
		} else {
			// Store the raw expression source text for runtime evaluation
			rng := attr.Expr.Range()
			if rng.Filename != "" {
				data, err := os.ReadFile(rng.Filename)
				if err == nil && rng.Start.Byte < len(data) && rng.End.Byte <= len(data) {
					source := string(data[rng.Start.Byte:rng.End.Byte])
					// Strip HCL string quotes from the source text
					if len(source) >= 2 && source[0] == '"' && source[len(source)-1] == '"' {
						source = source[1 : len(source)-1]
					}
					hook.Error = source
				}
			}
		}
	}

	// Parse modules
	for _, modBlock := range content.Blocks.OfType("module") {
		module, modDiags := p.parseModule(modBlock)
		diags = append(diags, modDiags...)
		if module != nil {
			hook.Modules = append(hook.Modules, *module)
		}
	}

	// Parse outputs - can be either an attribute (outputs = {...}) or a block (outputs {...})
	if attr, ok := content.Attributes["outputs"]; ok {
		// Attribute syntax: outputs = { ... }
		hook.OutputsExpr = attr.Expr
	} else if outputsBlocks := content.Blocks.OfType("outputs"); len(outputsBlocks) > 0 {
		// Block syntax: outputs { ... } - only process first block
		outputsBlock := outputsBlocks[0]
		attrs, attrDiags := outputsBlock.Body.JustAttributes()
		diags = append(diags, attrDiags...)
		if len(attrs) > 0 {
			hook.OutputsAttrs = attrs
		}
	}

	// Validate mutual exclusivity: error hooks must not have modules or outputs
	hasError := hook.ErrorExpr != nil
	hasModules := len(hook.Modules) > 0
	hasOutputs := hook.OutputsExpr != nil || hook.OutputsAttrs != nil

	if hasError && hasModules {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid hook: 'error' and 'module' are mutually exclusive",
			Detail:   "A hook with an 'error' attribute rejects the resource with a message and cannot also define modules to execute.",
			Subject:  block.DefRange.Ptr(),
		})
	}

	if hasError && hasOutputs {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid hook: 'error' and 'outputs' are mutually exclusive",
			Detail:   "A hook with an 'error' attribute rejects the resource with a message and cannot also define outputs.",
			Subject:  block.DefRange.Ptr(),
		})
	}

	return hook, diags
}
