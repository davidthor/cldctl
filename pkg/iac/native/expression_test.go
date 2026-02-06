package native

import (
	"reflect"
	"testing"
)

func TestEvaluateExpression_NoExpression(t *testing.T) {
	ctx := &EvalContext{
		Inputs:    map[string]interface{}{},
		Resources: map[string]*ResourceState{},
	}

	result, err := evaluateExpression("plain string", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "plain string" {
		t.Errorf("expected 'plain string', got %v", result)
	}
}

func TestEvaluateExpression_InputReference(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{
			"name": "test-value",
		},
		Resources: map[string]*ResourceState{},
	}

	result, err := evaluateExpression("${inputs.name}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "test-value" {
		t.Errorf("expected 'test-value', got %v", result)
	}
}

func TestEvaluateExpression_NestedInputReference(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{
			"config": map[string]interface{}{
				"nested": "nested-value",
			},
		},
		Resources: map[string]*ResourceState{},
	}

	result, err := evaluateExpression("${inputs.config.nested}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "nested-value" {
		t.Errorf("expected 'nested-value', got %v", result)
	}
}

func TestEvaluateExpression_ResourceOutput(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{},
		Resources: map[string]*ResourceState{
			"mycontainer": {
				Type: "docker:container",
				ID:   "container-123",
				Outputs: map[string]interface{}{
					"container_id": "container-123",
					"port":         8080,
				},
			},
		},
	}

	result, err := evaluateExpression("${resources.mycontainer.outputs.container_id}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "container-123" {
		t.Errorf("expected 'container-123', got %v", result)
	}
}

func TestEvaluateExpression_ResourceID(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{},
		Resources: map[string]*ResourceState{
			"mynetwork": {
				Type: "docker:network",
				ID:   "network-456",
			},
		},
	}

	result, err := evaluateExpression("${resources.mynetwork.id}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "network-456" {
		t.Errorf("expected 'network-456', got %v", result)
	}
}

func TestEvaluateExpression_ResourceProperties(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{},
		Resources: map[string]*ResourceState{
			"mycontainer": {
				Type: "docker:container",
				ID:   "container-123",
				Properties: map[string]interface{}{
					"image": "nginx:latest",
					"name":  "my-container",
				},
			},
		},
	}

	result, err := evaluateExpression("${resources.mycontainer.properties.image}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "nginx:latest" {
		t.Errorf("expected 'nginx:latest', got %v", result)
	}
}

func TestEvaluateExpression_Interpolation(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{
			"host": "localhost",
			"port": 8080,
		},
		Resources: map[string]*ResourceState{},
	}

	result, err := evaluateExpression("http://${inputs.host}:${inputs.port}/api", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "http://localhost:8080/api" {
		t.Errorf("expected 'http://localhost:8080/api', got %v", result)
	}
}

func TestEvaluateExpression_InvalidInputReference(t *testing.T) {
	ctx := &EvalContext{
		Inputs:    map[string]interface{}{},
		Resources: map[string]*ResourceState{},
	}

	_, err := evaluateExpression("${inputs.nonexistent}", ctx)
	if err == nil {
		t.Error("expected error for non-existent input")
	}
}

func TestEvaluateExpression_InvalidResourceReference(t *testing.T) {
	ctx := &EvalContext{
		Inputs:    map[string]interface{}{},
		Resources: map[string]*ResourceState{},
	}

	_, err := evaluateExpression("${resources.nonexistent.outputs.value}", ctx)
	if err == nil {
		t.Error("expected error for non-existent resource")
	}
}

func TestResolveReference_EmptyReference(t *testing.T) {
	ctx := &EvalContext{
		Inputs:    map[string]interface{}{},
		Resources: map[string]*ResourceState{},
	}

	_, err := resolveReference("", ctx)
	if err == nil {
		t.Error("expected error for empty reference")
	}
}

func TestResolveReference_InvalidInputFormat(t *testing.T) {
	ctx := &EvalContext{
		Inputs:    map[string]interface{}{},
		Resources: map[string]*ResourceState{},
	}

	_, err := resolveReference("inputs", ctx)
	if err == nil {
		t.Error("expected error for invalid input format")
	}
}

func TestResolveReference_InvalidResourceFormat(t *testing.T) {
	ctx := &EvalContext{
		Inputs:    map[string]interface{}{},
		Resources: map[string]*ResourceState{},
	}

	_, err := resolveReference("resources.name", ctx)
	if err == nil {
		t.Error("expected error for invalid resource format")
	}
}

func TestResolveReference_InvalidResourceProperty(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{},
		Resources: map[string]*ResourceState{
			"myresource": {
				Type: "docker:container",
				ID:   "123",
			},
		},
	}

	_, err := resolveReference("resources.myresource.invalid", ctx)
	if err == nil {
		t.Error("expected error for invalid resource property")
	}
}

func TestNavigatePath_EmptyPath(t *testing.T) {
	data := map[string]interface{}{
		"key": "value",
	}

	result, err := navigatePath(data, []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !reflect.DeepEqual(result, data) {
		t.Errorf("expected original data, got %v", result)
	}
}

func TestNavigatePath_SingleKey(t *testing.T) {
	data := map[string]interface{}{
		"key": "value",
	}

	result, err := navigatePath(data, []string{"key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "value" {
		t.Errorf("expected 'value', got %v", result)
	}
}

func TestNavigatePath_NestedKeys(t *testing.T) {
	data := map[string]interface{}{
		"level1": map[string]interface{}{
			"level2": map[string]interface{}{
				"key": "deep-value",
			},
		},
	}

	result, err := navigatePath(data, []string{"level1", "level2", "key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "deep-value" {
		t.Errorf("expected 'deep-value', got %v", result)
	}
}

func TestNavigatePath_StringMap(t *testing.T) {
	data := map[string]string{
		"key": "string-value",
	}

	result, err := navigatePath(data, []string{"key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "string-value" {
		t.Errorf("expected 'string-value', got %v", result)
	}
}

func TestNavigatePath_KeyNotFound(t *testing.T) {
	data := map[string]interface{}{
		"key": "value",
	}

	_, err := navigatePath(data, []string{"nonexistent"})
	if err == nil {
		t.Error("expected error for non-existent key")
	}
}

func TestNavigatePath_InvalidType(t *testing.T) {
	data := "string-value"

	_, err := navigatePath(data, []string{"key"})
	if err == nil {
		t.Error("expected error for invalid type")
	}
}

func TestEvaluateFunction_RandomPassword(t *testing.T) {
	ctx := &EvalContext{
		Inputs:    map[string]interface{}{},
		Resources: map[string]*ResourceState{},
	}

	result, err := evaluateFunction("random_password(16)", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	strResult, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}

	if len(strResult) != 16 {
		t.Errorf("expected 16 character string, got %d", len(strResult))
	}
}

func TestEvaluateFunction_UnknownFunction(t *testing.T) {
	ctx := &EvalContext{
		Inputs:    map[string]interface{}{},
		Resources: map[string]*ResourceState{},
	}

	_, err := evaluateFunction("unknown_function()", ctx)
	if err == nil {
		t.Error("expected error for unknown function")
	}
}

func TestGenerateRandomString(t *testing.T) {
	result := generateRandomString(10)
	if len(result) != 10 {
		t.Errorf("expected 10 character string, got %d", len(result))
	}

	// Verify it only contains expected characters
	validChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for _, c := range result {
		found := false
		for _, v := range validChars {
			if c == v {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unexpected character %c in random string", c)
		}
	}
}

func TestResolveProperties(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{
			"image": "nginx:latest",
			"port":  8080,
		},
		Resources: map[string]*ResourceState{},
	}

	props := map[string]interface{}{
		"image":  "${inputs.image}",
		"port":   "${inputs.port}",
		"static": "unchanged",
	}

	result, err := resolveProperties(props, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["image"] != "nginx:latest" {
		t.Errorf("expected image='nginx:latest', got %v", result["image"])
	}
	if result["port"] != 8080 {
		t.Errorf("expected port=8080, got %v", result["port"])
	}
	if result["static"] != "unchanged" {
		t.Errorf("expected static='unchanged', got %v", result["static"])
	}
}

func TestResolveProperties_Error(t *testing.T) {
	ctx := &EvalContext{
		Inputs:    map[string]interface{}{},
		Resources: map[string]*ResourceState{},
	}

	props := map[string]interface{}{
		"invalid": "${inputs.nonexistent}",
	}

	_, err := resolveProperties(props, ctx)
	if err == nil {
		t.Error("expected error for invalid property")
	}
}

func TestResolveValue_String(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{
			"value": "test",
		},
		Resources: map[string]*ResourceState{},
	}

	result, err := resolveValue("${inputs.value}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "test" {
		t.Errorf("expected 'test', got %v", result)
	}
}

func TestResolveValue_Map(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{
			"value": "resolved",
		},
		Resources: map[string]*ResourceState{},
	}

	input := map[string]interface{}{
		"key": "${inputs.value}",
	}

	result, err := resolveValue(input, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}

	if resultMap["key"] != "resolved" {
		t.Errorf("expected key='resolved', got %v", resultMap["key"])
	}
}

func TestResolveValue_Slice(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{
			"value1": "first",
			"value2": "second",
		},
		Resources: map[string]*ResourceState{},
	}

	input := []interface{}{"${inputs.value1}", "${inputs.value2}"}

	result, err := resolveValue(input, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultSlice, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected slice, got %T", result)
	}

	if len(resultSlice) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(resultSlice))
	}

	if resultSlice[0] != "first" {
		t.Errorf("expected first element 'first', got %v", resultSlice[0])
	}
	if resultSlice[1] != "second" {
		t.Errorf("expected second element 'second', got %v", resultSlice[1])
	}
}

func TestResolveValue_NonString(t *testing.T) {
	ctx := &EvalContext{
		Inputs:    map[string]interface{}{},
		Resources: map[string]*ResourceState{},
	}

	// Test various non-string types
	tests := []interface{}{
		42,
		3.14,
		true,
		nil,
	}

	for _, input := range tests {
		result, err := resolveValue(input, ctx)
		if err != nil {
			t.Errorf("unexpected error for %v: %v", input, err)
		}
		if result != input {
			t.Errorf("expected %v, got %v", input, result)
		}
	}
}

func TestResolveValue_NestedMap(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{
			"deep": "resolved-deep",
		},
		Resources: map[string]*ResourceState{},
	}

	input := map[string]interface{}{
		"level1": map[string]interface{}{
			"level2": "${inputs.deep}",
		},
	}

	result, err := resolveValue(input, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resultMap := result.(map[string]interface{})
	level1 := resultMap["level1"].(map[string]interface{})

	if level1["level2"] != "resolved-deep" {
		t.Errorf("expected level2='resolved-deep', got %v", level1["level2"])
	}
}

func TestLookupPort_Found(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{},
		Resources: map[string]*ResourceState{
			"myservice": {
				Outputs: map[string]interface{}{
					"ports": map[string]interface{}{
						"8080/tcp": 32001,
					},
				},
			},
		},
	}

	result, err := evaluateFunction(`lookup_port("myservice", "8080")`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 32001 {
		t.Errorf("expected 32001, got %v", result)
	}
}

func TestLookupPort_PortWithProtocol(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{},
		Resources: map[string]*ResourceState{
			"myservice": {
				Outputs: map[string]interface{}{
					"ports": map[string]interface{}{
						"8080/tcp": 32002,
					},
				},
			},
		},
	}

	result, err := evaluateFunction(`lookup_port("myservice", "8080/tcp")`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 32002 {
		t.Errorf("expected 32002, got %v", result)
	}
}

func TestLookupPort_ResourceNotFound(t *testing.T) {
	ctx := &EvalContext{
		Inputs:    map[string]interface{}{},
		Resources: map[string]*ResourceState{},
	}

	// Should fall back to returning the port argument
	result, err := evaluateFunction(`lookup_port("unknown", "8080")`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "8080" {
		t.Errorf("expected fallback '8080', got %v", result)
	}
}

func TestLookupPort_NoPortsOutput(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{},
		Resources: map[string]*ResourceState{
			"myservice": {
				Outputs: map[string]interface{}{
					"id": "container-123",
				},
			},
		},
	}

	// Should fall back to returning the port argument
	result, err := evaluateFunction(`lookup_port("myservice", "8080")`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "8080" {
		t.Errorf("expected fallback '8080', got %v", result)
	}
}

func TestLookupPort_PortNotMapped(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{},
		Resources: map[string]*ResourceState{
			"myservice": {
				Outputs: map[string]interface{}{
					"ports": map[string]interface{}{
						"3000/tcp": 32000,
					},
				},
			},
		},
	}

	// Port 8080 isn't mapped, should fall back
	result, err := evaluateFunction(`lookup_port("myservice", "8080")`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "8080" {
		t.Errorf("expected fallback '8080', got %v", result)
	}
}

func TestLookupPort_TooFewArgs(t *testing.T) {
	ctx := &EvalContext{
		Inputs:    map[string]interface{}{},
		Resources: map[string]*ResourceState{},
	}

	_, err := evaluateFunction(`lookup_port("myservice")`, ctx)
	if err == nil {
		t.Fatal("expected error for too few arguments")
	}
}
