package v1

import (
	"github.com/davidthor/cldctl/pkg/schema/environment/internal"
)

// Transformer converts v1 schema to internal representation.
type Transformer struct{}

// NewTransformer creates a new v1 environment transformer.
func NewTransformer() *Transformer {
	return &Transformer{}
}

// Transform converts a v1 schema to the internal representation.
func (t *Transformer) Transform(v1 *SchemaV1) (*internal.InternalEnvironment, error) {
	env := &internal.InternalEnvironment{
		Name:          v1.Name,
		Variables:     make(map[string]internal.InternalEnvironmentVariable),
		Locals:        v1.Locals,
		Components:    make(map[string]internal.InternalComponentConfig),
		SourceVersion: "v1",
	}

	// Transform variables
	for name, variable := range v1.Variables {
		env.Variables[name] = t.transformVariable(name, variable)
	}

	// Transform components
	for name, comp := range v1.Components {
		env.Components[name] = t.transformComponent(comp)
	}

	return env, nil
}

func (t *Transformer) transformVariable(name string, v1 EnvironmentVariableV1) internal.InternalEnvironmentVariable {
	return internal.InternalEnvironmentVariable{
		Name:        name,
		Description: v1.Description,
		Default:     v1.Default,
		Required:    v1.Required,
		Sensitive:   v1.Sensitive,
		Env:         v1.Env,
	}
}

func (t *Transformer) transformComponent(v1 ComponentConfigV1) internal.InternalComponentConfig {
	comp := internal.InternalComponentConfig{
		Path:        v1.Path,
		Image:       v1.Image,
		Variables:   v1.Variables,
		Ports:       v1.Ports,
		Scaling:     make(map[string]internal.InternalScalingConfig),
		Functions:   make(map[string]internal.InternalFunctionConfig),
		Environment: v1.Environment,
		Routes:      make(map[string]internal.InternalRouteConfig),
		Distinct:    v1.Distinct,
	}

	// Transform scaling configs
	for name, scaling := range v1.Scaling {
		comp.Scaling[name] = t.transformScaling(scaling)
	}

	// Transform function configs
	for name, funcConfig := range v1.Functions {
		comp.Functions[name] = t.transformFunction(funcConfig)
	}

	// Transform route configs
	for name, routeConfig := range v1.Routes {
		comp.Routes[name] = t.transformRoute(routeConfig)
	}

	// Transform instances
	for _, inst := range v1.Instances {
		comp.Instances = append(comp.Instances, t.transformInstance(inst))
	}

	return comp
}

func (t *Transformer) transformInstance(v1 InstanceConfigV1) internal.InternalInstanceConfig {
	return internal.InternalInstanceConfig{
		Name:      v1.Name,
		Source:    v1.Source,
		Weight:    v1.Weight,
		Variables: v1.Variables,
	}
}

func (t *Transformer) transformScaling(v1 ScalingConfigV1) internal.InternalScalingConfig {
	return internal.InternalScalingConfig{
		Replicas:    v1.Replicas,
		CPU:         v1.CPU,
		Memory:      v1.Memory,
		MinReplicas: v1.MinReplicas,
		MaxReplicas: v1.MaxReplicas,
	}
}

func (t *Transformer) transformFunction(v1 FunctionConfigV1) internal.InternalFunctionConfig {
	return internal.InternalFunctionConfig{
		Regions: v1.Regions,
		Memory:  v1.Memory,
		Timeout: v1.Timeout,
	}
}

func (t *Transformer) transformRoute(v1 RouteConfigV1) internal.InternalRouteConfig {
	route := internal.InternalRouteConfig{
		Subdomain:  v1.Subdomain,
		PathPrefix: v1.PathPrefix,
		Hostnames:  make([]internal.InternalHostname, len(v1.Hostnames)),
	}

	for i, hostname := range v1.Hostnames {
		route.Hostnames[i] = internal.InternalHostname{
			Subdomain: hostname.Subdomain,
			Host:      hostname.Host,
		}
	}

	if v1.TLS != nil {
		route.TLS = &internal.InternalTLSConfig{
			Enabled:    v1.TLS.Enabled,
			SecretName: v1.TLS.SecretName,
		}
	}

	return route
}
