package v1

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// standardFunctions returns the standard HCL functions available in datacenter configs.
func standardFunctions() map[string]function.Function {
	return map[string]function.Function{
		// String functions
		"upper":      stdlib.UpperFunc,
		"lower":      stdlib.LowerFunc,
		"trim":       stdlib.TrimFunc,
		"trimprefix": stdlib.TrimPrefixFunc,
		"trimsuffix": stdlib.TrimSuffixFunc,
		"trimspace":  stdlib.TrimSpaceFunc,
		"replace":    stdlib.ReplaceFunc,
		"split":      stdlib.SplitFunc,
		"join":       stdlib.JoinFunc,
		"substr":     stdlib.SubstrFunc,
		"strlen":     stdlib.StrlenFunc,
		"chomp":      stdlib.ChompFunc,
		"indent":     stdlib.IndentFunc,
		"format":     stdlib.FormatFunc,
		"formatlist": stdlib.FormatListFunc,
		"regex":      stdlib.RegexFunc,
		"regexall":   stdlib.RegexAllFunc,
		"startswith": startsWithFunc,
		"endswith":   endsWithFunc,

		// Collection functions
		"length":   stdlib.LengthFunc,
		"element":  stdlib.ElementFunc,
		"coalesce": stdlib.CoalesceFunc,
		"compact":  stdlib.CompactFunc,
		"concat":   stdlib.ConcatFunc,
		"contains": stdlib.ContainsFunc,
		"distinct": stdlib.DistinctFunc,
		"flatten":  stdlib.FlattenFunc,
		"keys":     stdlib.KeysFunc,
		"values":   stdlib.ValuesFunc,
		"lookup":   stdlib.LookupFunc,
		"merge":    stdlib.MergeFunc,
		"range":    stdlib.RangeFunc,
		"reverse":  stdlib.ReverseFunc,
		"slice":    stdlib.SliceFunc,
		"sort":     stdlib.SortFunc,
		"zipmap":   stdlib.ZipmapFunc,

		// Numeric functions
		"abs":      stdlib.AbsoluteFunc,
		"ceil":     stdlib.CeilFunc,
		"floor":    stdlib.FloorFunc,
		"max":      stdlib.MaxFunc,
		"min":      stdlib.MinFunc,
		"parseint": stdlib.ParseIntFunc,
		"signum":   stdlib.SignumFunc,

		// Type conversion
		"tobool":   stdlib.MakeToFunc(cty.Bool),
		"tolist":   stdlib.MakeToFunc(cty.List(cty.DynamicPseudoType)),
		"tomap":    stdlib.MakeToFunc(cty.Map(cty.DynamicPseudoType)),
		"tonumber": stdlib.MakeToFunc(cty.Number),
		"toset":    stdlib.MakeToFunc(cty.Set(cty.DynamicPseudoType)),
		"tostring": stdlib.MakeToFunc(cty.String),

		// Encoding/Decoding
		"base64encode": base64EncodeFunc,
		"base64decode": base64DecodeFunc,
		"jsonencode":   jsonEncodeFunc,
		"jsondecode":   jsonDecodeFunc,

		// Conditional
		"try": tryFunc,

		// Custom utility functions
		"env":     envFunc,
		"default": defaultFunc,
	}
}

// base64EncodeFunc encodes a string to base64.
var base64EncodeFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "str", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		str := args[0].AsString()
		return cty.StringVal(base64.StdEncoding.EncodeToString([]byte(str))), nil
	},
})

// base64DecodeFunc decodes a base64 string.
var base64DecodeFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "str", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		str := args[0].AsString()
		decoded, err := base64.StdEncoding.DecodeString(str)
		if err != nil {
			return cty.UnknownVal(cty.String), fmt.Errorf("invalid base64: %w", err)
		}
		return cty.StringVal(string(decoded)), nil
	},
})

// jsonEncodeFunc encodes a value to JSON.
var jsonEncodeFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "val", Type: cty.DynamicPseudoType},
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		val := fromCtyValue(args[0])
		jsonBytes, err := json.Marshal(val)
		if err != nil {
			return cty.UnknownVal(cty.String), fmt.Errorf("json encode failed: %w", err)
		}
		return cty.StringVal(string(jsonBytes)), nil
	},
})

// jsonDecodeFunc decodes a JSON string.
var jsonDecodeFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "str", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.DynamicPseudoType),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		str := args[0].AsString()
		var val interface{}
		if err := json.Unmarshal([]byte(str), &val); err != nil {
			return cty.NilVal, fmt.Errorf("json decode failed: %w", err)
		}
		return toCtyValue(val), nil
	},
})

// tryFunc returns the first non-error value.
var tryFunc = function.New(&function.Spec{
	VarParam: &function.Parameter{
		Name: "expressions",
		Type: cty.DynamicPseudoType,
	},
	Type: function.StaticReturnType(cty.DynamicPseudoType),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		for _, arg := range args {
			if !arg.IsNull() && arg.IsKnown() {
				return arg, nil
			}
		}
		return cty.NilVal, fmt.Errorf("all expressions evaluated to null")
	},
})

// envFunc returns an environment variable value from the system environment.
// If the variable is not set, it returns an empty string.
// Use default(env("VAR"), "fallback") to provide a default value.
var envFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "name", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		name := args[0].AsString()
		value := os.Getenv(name)
		return cty.StringVal(value), nil
	},
})

// defaultFunc returns the first non-null/non-empty value.
var defaultFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "value", Type: cty.DynamicPseudoType},
		{Name: "default", Type: cty.DynamicPseudoType},
	},
	Type: function.StaticReturnType(cty.DynamicPseudoType),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		val := args[0]
		def := args[1]

		if val.IsNull() || !val.IsKnown() {
			return def, nil
		}

		// Check for empty string
		if val.Type() == cty.String && val.AsString() == "" {
			return def, nil
		}

		// Check for empty list/tuple
		if val.Type().IsListType() || val.Type().IsTupleType() {
			if val.LengthInt() == 0 {
				return def, nil
			}
		}

		return val, nil
	},
})

// startsWithFunc returns true if the string starts with the given prefix.
var startsWithFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "str", Type: cty.String},
		{Name: "prefix", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		str := args[0].AsString()
		prefix := args[1].AsString()
		return cty.BoolVal(strings.HasPrefix(str, prefix)), nil
	},
})

// endsWithFunc returns true if the string ends with the given suffix.
var endsWithFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "str", Type: cty.String},
		{Name: "suffix", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		str := args[0].AsString()
		suffix := args[1].AsString()
		return cty.BoolVal(strings.HasSuffix(str, suffix)), nil
	},
})

// BuildInputsFromAttributes extracts inputs from HCL attributes with context.
func BuildInputsFromAttributes(attrs hcl.Attributes, ctx *hcl.EvalContext) (map[string]cty.Value, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	result := make(map[string]cty.Value)

	for name, attr := range attrs {
		val, valDiags := attr.Expr.Value(ctx)
		diags = append(diags, valDiags...)
		if !valDiags.HasErrors() {
			result[name] = val
		}
	}

	return result, diags
}

// EvaluateTemplateString evaluates a string that may contain ${...} template expressions.
func EvaluateTemplateString(s string, ctx *EvalContext) (string, error) {
	// If no template syntax, return as-is
	if !strings.Contains(s, "${") {
		return s, nil
	}

	// Use HCL's template syntax parser
	hclCtx := ctx.ToHCLContext()

	// Simple variable replacement for ${var.name} syntax
	result := s
	for name, val := range ctx.Variables {
		placeholder := fmt.Sprintf("${variable.%s}", name)
		if strings.Contains(result, placeholder) {
			if val.Type() == cty.String {
				result = strings.ReplaceAll(result, placeholder, val.AsString())
			}
		}
		// Also support var. alias
		placeholder = fmt.Sprintf("${var.%s}", name)
		if strings.Contains(result, placeholder) {
			if val.Type() == cty.String {
				result = strings.ReplaceAll(result, placeholder, val.AsString())
			}
		}
	}

	// Environment references
	if ctx.Environment != nil {
		result = strings.ReplaceAll(result, "${environment.name}", ctx.Environment.Name)
		result = strings.ReplaceAll(result, "${environment.datacenter}", ctx.Environment.Datacenter)
	}

	// Node references
	if ctx.Node != nil {
		result = strings.ReplaceAll(result, "${node.name}", ctx.Node.Name)
		result = strings.ReplaceAll(result, "${node.component}", ctx.Node.Component)
		result = strings.ReplaceAll(result, "${node.type}", ctx.Node.Type)
	}

	// Module references
	for modName, modOutputs := range ctx.Modules {
		if modOutputs.Type().IsObjectType() {
			for it := modOutputs.ElementIterator(); it.Next(); {
				key, val := it.Element()
				placeholder := fmt.Sprintf("${module.%s.%s}", modName, key.AsString())
				if strings.Contains(result, placeholder) && val.Type() == cty.String {
					result = strings.ReplaceAll(result, placeholder, val.AsString())
				}
			}
		}
	}

	// For more complex expressions, we'd need to parse as HCL template
	_ = hclCtx // Reserved for future complex template parsing

	return result, nil
}
