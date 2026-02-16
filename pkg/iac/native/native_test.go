package native

import (
	"testing"
)

func TestGetString(t *testing.T) {
	props := map[string]interface{}{
		"name":   "test-name",
		"number": 42,
		"nil":    nil,
	}

	// Test existing string
	if result := getString(props, "name"); result != "test-name" {
		t.Errorf("expected 'test-name', got %q", result)
	}

	// Test non-existent key
	if result := getString(props, "nonexistent"); result != "" {
		t.Errorf("expected empty string, got %q", result)
	}

	// Test non-string value
	if result := getString(props, "number"); result != "" {
		t.Errorf("expected empty string for non-string value, got %q", result)
	}

	// Test nil value
	if result := getString(props, "nil"); result != "" {
		t.Errorf("expected empty string for nil value, got %q", result)
	}
}

func TestGetStringSlice(t *testing.T) {
	props := map[string]interface{}{
		"commands": []interface{}{"ls", "-la", "/home"},
		"empty":    []interface{}{},
		"mixed":    []interface{}{"string", 42, true},
		"notArray": "string",
	}

	// Test existing string slice
	result := getStringSlice(props, "commands")
	if len(result) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result))
	}
	if result[0] != "ls" || result[1] != "-la" || result[2] != "/home" {
		t.Errorf("unexpected result: %v", result)
	}

	// Test empty slice
	result = getStringSlice(props, "empty")
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %v", result)
	}

	// Test mixed types (non-strings become empty strings)
	result = getStringSlice(props, "mixed")
	if len(result) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result))
	}
	if result[0] != "string" {
		t.Errorf("expected first element 'string', got %q", result[0])
	}

	// Test non-existent key
	result = getStringSlice(props, "nonexistent")
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}

	// Test string value (should be wrapped with sh -c for shell execution)
	result = getStringSlice(props, "notArray")
	if len(result) != 3 || result[0] != "sh" || result[1] != "-c" || result[2] != "string" {
		t.Errorf("expected [sh -c string] for string value, got %v", result)
	}
}

func TestGetStringMap(t *testing.T) {
	props := map[string]interface{}{
		"env": map[string]interface{}{
			"KEY1": "value1",
			"KEY2": "value2",
		},
		"empty":  map[string]interface{}{},
		"mixed":  map[string]interface{}{"string": "value", "number": 42},
		"notMap": "string",
	}

	// Test existing string map
	result := getStringMap(props, "env")
	if len(result) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(result))
	}
	if result["KEY1"] != "value1" || result["KEY2"] != "value2" {
		t.Errorf("unexpected result: %v", result)
	}

	// Test empty map
	result = getStringMap(props, "empty")
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}

	// Test mixed types
	result = getStringMap(props, "mixed")
	if result["string"] != "value" {
		t.Errorf("expected string='value', got %q", result["string"])
	}

	// Test non-existent key
	result = getStringMap(props, "nonexistent")
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}

	// Test non-map value
	result = getStringMap(props, "notMap")
	if result != nil {
		t.Errorf("expected nil for non-map, got %v", result)
	}
}

func TestGetPortMappings(t *testing.T) {
	props := map[string]interface{}{
		"ports": []interface{}{
			map[string]interface{}{
				"container": 8080,
				"host":      8080,
			},
			map[string]interface{}{
				"container": 443,
				"host":      0, // 0 = let Docker assign a host port
			},
		},
		"empty":    []interface{}{},
		"notArray": "string",
	}

	// Test port mappings
	result := getPortMappings(props, "ports")
	if len(result) != 2 {
		t.Fatalf("expected 2 port mappings, got %d", len(result))
	}

	if result[0].ContainerPort != 8080 || result[0].HostPort != 8080 {
		t.Errorf("unexpected first port mapping: %+v", result[0])
	}

	if result[1].ContainerPort != 443 || result[1].HostPort != 0 {
		t.Errorf("unexpected second port mapping: %+v", result[1])
	}

	// Test empty
	result = getPortMappings(props, "empty")
	if result != nil {
		t.Errorf("expected nil for empty array, got %v", result)
	}

	// Test non-existent key
	result = getPortMappings(props, "nonexistent")
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}

	// Test non-array value
	result = getPortMappings(props, "notArray")
	if result != nil {
		t.Errorf("expected nil for non-array, got %v", result)
	}
}

func TestGetVolumeMounts(t *testing.T) {
	props := map[string]interface{}{
		"volumes": []interface{}{
			map[string]interface{}{
				"name":   "data-volume",
				"source": "/host/data",
				"path":   "/container/data",
			},
			map[string]interface{}{
				"name": "named-volume",
				"path": "/container/named",
			},
		},
		"empty":    []interface{}{},
		"notArray": "string",
	}

	// Test volume mounts
	result := getVolumeMounts(props, "volumes")
	if len(result) != 2 {
		t.Fatalf("expected 2 volume mounts, got %d", len(result))
	}

	if result[0].Name != "data-volume" || result[0].Source != "/host/data" || result[0].Path != "/container/data" {
		t.Errorf("unexpected first volume mount: %+v", result[0])
	}

	if result[1].Name != "named-volume" || result[1].Path != "/container/named" {
		t.Errorf("unexpected second volume mount: %+v", result[1])
	}

	// Test empty
	result = getVolumeMounts(props, "empty")
	if result != nil {
		t.Errorf("expected nil for empty array, got %v", result)
	}

	// Test non-existent key
	result = getVolumeMounts(props, "nonexistent")
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}

	// Test non-array value
	result = getVolumeMounts(props, "notArray")
	if result != nil {
		t.Errorf("expected nil for non-array, got %v", result)
	}
}

func TestGetString2(t *testing.T) {
	m := map[string]interface{}{
		"name":   "test-name",
		"number": 42,
	}

	// Test existing string
	if result := getString2(m, "name"); result != "test-name" {
		t.Errorf("expected 'test-name', got %q", result)
	}

	// Test non-existent key
	if result := getString2(m, "nonexistent"); result != "" {
		t.Errorf("expected empty string, got %q", result)
	}

	// Test non-string value
	if result := getString2(m, "number"); result != "" {
		t.Errorf("expected empty string for non-string value, got %q", result)
	}
}

func TestPlugin_Name(t *testing.T) {
	// We can't test NewPlugin without Docker, but we can test the Name method
	// if we construct the plugin manually
	p := &Plugin{}
	if p.Name() != "native" {
		t.Errorf("expected 'native', got %q", p.Name())
	}
}

func TestPlugin_ResolveInputs(t *testing.T) {
	p := &Plugin{}

	defs := map[string]InputDef{
		"required": {
			Type:     "string",
			Required: true,
		},
		"optional": {
			Type:     "string",
			Required: false,
			Default:  "default-value",
		},
		"provided_optional": {
			Type:     "string",
			Required: false,
			Default:  "default-value",
		},
	}

	provided := map[string]interface{}{
		"required":          "provided-required",
		"provided_optional": "provided-optional",
	}

	result, err := p.resolveInputs(defs, provided)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["required"] != "provided-required" {
		t.Errorf("expected required='provided-required', got %v", result["required"])
	}

	if result["optional"] != "default-value" {
		t.Errorf("expected optional='default-value', got %v", result["optional"])
	}

	if result["provided_optional"] != "provided-optional" {
		t.Errorf("expected provided_optional='provided-optional', got %v", result["provided_optional"])
	}
}

func TestPlugin_ResolveInputs_MissingRequired(t *testing.T) {
	p := &Plugin{}

	defs := map[string]InputDef{
		"required": {
			Type:     "string",
			Required: true,
		},
	}

	provided := map[string]interface{}{}

	_, err := p.resolveInputs(defs, provided)
	if err == nil {
		t.Error("expected error for missing required input")
	}
}

func TestPortMapping_Struct(t *testing.T) {
	pm := PortMapping{
		ContainerPort: 8080,
		HostPort:      8080,
		Protocol:      "tcp",
	}

	if pm.ContainerPort != 8080 {
		t.Errorf("expected ContainerPort=8080, got %d", pm.ContainerPort)
	}
	if pm.HostPort != 8080 {
		t.Errorf("expected HostPort=8080, got %d", pm.HostPort)
	}
	if pm.Protocol != "tcp" {
		t.Errorf("expected Protocol='tcp', got %q", pm.Protocol)
	}
}

func TestVolumeMount_Struct(t *testing.T) {
	vm := VolumeMount{
		Name:   "data-volume",
		Source: "/host/path",
		Path:   "/container/path",
	}

	if vm.Name != "data-volume" {
		t.Errorf("expected Name='data-volume', got %q", vm.Name)
	}
	if vm.Source != "/host/path" {
		t.Errorf("expected Source='/host/path', got %q", vm.Source)
	}
	if vm.Path != "/container/path" {
		t.Errorf("expected Path='/container/path', got %q", vm.Path)
	}
}

func TestHealthcheck_Struct(t *testing.T) {
	hc := Healthcheck{
		Command:  []string{"curl", "-f", "http://localhost/health"},
		Interval: "30s",
		Timeout:  "10s",
		Retries:  3,
	}

	if len(hc.Command) != 3 {
		t.Errorf("expected 3 command parts, got %d", len(hc.Command))
	}
	if hc.Interval != "30s" {
		t.Errorf("expected Interval='30s', got %q", hc.Interval)
	}
	if hc.Timeout != "10s" {
		t.Errorf("expected Timeout='10s', got %q", hc.Timeout)
	}
	if hc.Retries != 3 {
		t.Errorf("expected Retries=3, got %d", hc.Retries)
	}
}

func TestContainerOptions_Struct(t *testing.T) {
	opts := ContainerOptions{
		Image:      "nginx:latest",
		Name:       "my-container",
		Command:    []string{"nginx", "-g", "daemon off;"},
		Entrypoint: []string{"/docker-entrypoint.sh"},
		Environment: map[string]string{
			"ENV_VAR": "value",
		},
		Ports: []PortMapping{
			{ContainerPort: 80, HostPort: 8080},
		},
		Volumes: []VolumeMount{
			{Name: "data", Path: "/data"},
		},
		Network: "my-network",
		Restart: "unless-stopped",
		Healthcheck: &Healthcheck{
			Command:  []string{"curl", "-f", "http://localhost/"},
			Interval: "30s",
			Timeout:  "10s",
			Retries:  3,
		},
	}

	if opts.Image != "nginx:latest" {
		t.Errorf("expected Image='nginx:latest', got %q", opts.Image)
	}
	if opts.Name != "my-container" {
		t.Errorf("expected Name='my-container', got %q", opts.Name)
	}
	if len(opts.Command) != 3 {
		t.Errorf("expected 3 command parts, got %d", len(opts.Command))
	}
	if len(opts.Entrypoint) != 1 {
		t.Errorf("expected 1 entrypoint, got %d", len(opts.Entrypoint))
	}
	if opts.Environment["ENV_VAR"] != "value" {
		t.Errorf("expected ENV_VAR='value', got %q", opts.Environment["ENV_VAR"])
	}
	if len(opts.Ports) != 1 {
		t.Errorf("expected 1 port mapping, got %d", len(opts.Ports))
	}
	if len(opts.Volumes) != 1 {
		t.Errorf("expected 1 volume mount, got %d", len(opts.Volumes))
	}
	if opts.Network != "my-network" {
		t.Errorf("expected Network='my-network', got %q", opts.Network)
	}
	if opts.Restart != "unless-stopped" {
		t.Errorf("expected Restart='unless-stopped', got %q", opts.Restart)
	}
	if opts.Healthcheck == nil {
		t.Error("expected Healthcheck to be set")
	}
}

func TestContainerInfo_Struct(t *testing.T) {
	info := ContainerInfo{
		ID:   "container-123",
		Name: "my-container",
		Ports: map[string]int{
			"80/tcp":  8080,
			"443/tcp": 8443,
		},
	}

	if info.ID != "container-123" {
		t.Errorf("expected ID='container-123', got %q", info.ID)
	}
	if info.Name != "my-container" {
		t.Errorf("expected Name='my-container', got %q", info.Name)
	}
	if len(info.Ports) != 2 {
		t.Errorf("expected 2 ports, got %d", len(info.Ports))
	}
	if info.Ports["80/tcp"] != 8080 {
		t.Errorf("expected port 80/tcp=8080, got %d", info.Ports["80/tcp"])
	}
}

func TestGetHealthcheck_FromProps(t *testing.T) {
	tests := []struct {
		name    string
		props   map[string]interface{}
		wantNil bool
		check   func(t *testing.T, hc *Healthcheck)
	}{
		{
			name:    "nil when key missing",
			props:   map[string]interface{}{},
			wantNil: true,
		},
		{
			name:    "nil when value is nil",
			props:   map[string]interface{}{"healthcheck": nil},
			wantNil: true,
		},
		{
			name:    "nil when value is wrong type",
			props:   map[string]interface{}{"healthcheck": "not-a-map"},
			wantNil: true,
		},
		{
			name: "nil when command is empty",
			props: map[string]interface{}{
				"healthcheck": map[string]interface{}{
					"interval": "5s",
					"retries":  3,
				},
			},
			wantNil: true,
		},
		{
			name: "parses full healthcheck",
			props: map[string]interface{}{
				"healthcheck": map[string]interface{}{
					"command":  []interface{}{"pg_isready", "-U", "app"},
					"interval": "5s",
					"timeout":  "3s",
					"retries":  10,
				},
			},
			check: func(t *testing.T, hc *Healthcheck) {
				if len(hc.Command) != 3 || hc.Command[0] != "pg_isready" {
					t.Errorf("expected command [pg_isready -U app], got %v", hc.Command)
				}
				if hc.Interval != "5s" {
					t.Errorf("expected interval '5s', got %q", hc.Interval)
				}
				if hc.Timeout != "3s" {
					t.Errorf("expected timeout '3s', got %q", hc.Timeout)
				}
				if hc.Retries != 10 {
					t.Errorf("expected retries 10, got %d", hc.Retries)
				}
			},
		},
		{
			name: "parses retries as float64 (JSON deserialization)",
			props: map[string]interface{}{
				"healthcheck": map[string]interface{}{
					"command":  []interface{}{"curl", "-f", "http://localhost/"},
					"interval": "10s",
					"timeout":  "5s",
					"retries":  float64(5),
				},
			},
			check: func(t *testing.T, hc *Healthcheck) {
				if hc.Retries != 5 {
					t.Errorf("expected retries 5 (from float64), got %d", hc.Retries)
				}
			},
		},
		{
			name: "parses minimal healthcheck (command only)",
			props: map[string]interface{}{
				"healthcheck": map[string]interface{}{
					"command": []interface{}{"true"},
				},
			},
			check: func(t *testing.T, hc *Healthcheck) {
				if len(hc.Command) != 1 || hc.Command[0] != "true" {
					t.Errorf("expected command [true], got %v", hc.Command)
				}
				if hc.Interval != "" {
					t.Errorf("expected empty interval, got %q", hc.Interval)
				}
				if hc.Retries != 0 {
					t.Errorf("expected retries 0, got %d", hc.Retries)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := getHealthcheck(tt.props, "healthcheck")
			if tt.wantNil {
				if hc != nil {
					t.Errorf("expected nil healthcheck, got %+v", hc)
				}
				return
			}
			if hc == nil {
				t.Fatal("expected non-nil healthcheck")
			}
			if tt.check != nil {
				tt.check(t, hc)
			}
		})
	}
}

func TestEvalContext_Struct(t *testing.T) {
	ctx := EvalContext{
		Inputs: map[string]interface{}{
			"image": "nginx:latest",
		},
		Resources: map[string]*ResourceState{
			"container": {
				Type: "docker:container",
				ID:   "container-123",
			},
		},
	}

	if ctx.Inputs["image"] != "nginx:latest" {
		t.Errorf("expected image='nginx:latest', got %v", ctx.Inputs["image"])
	}
	if ctx.Resources["container"].ID != "container-123" {
		t.Errorf("expected container ID='container-123', got %v", ctx.Resources["container"].ID)
	}
}
