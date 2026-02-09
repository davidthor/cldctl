package environment

import (
	"github.com/davidthor/cldctl/pkg/schema/environment/internal"
)

// environmentWrapper wraps an InternalEnvironment to implement the Environment interface.
type environmentWrapper struct {
	env *internal.InternalEnvironment
}

func (e *environmentWrapper) Variables() map[string]EnvironmentVariable {
	if e.env.Variables == nil {
		return nil
	}
	result := make(map[string]EnvironmentVariable, len(e.env.Variables))
	for name := range e.env.Variables {
		v := e.env.Variables[name]
		result[name] = &environmentVariableWrapper{v: &v}
	}
	return result
}

func (e *environmentWrapper) Locals() map[string]interface{} {
	return e.env.Locals
}

func (e *environmentWrapper) Components() map[string]ComponentConfig {
	result := make(map[string]ComponentConfig)
	for name := range e.env.Components {
		comp := e.env.Components[name]
		result[name] = &componentConfigWrapper{c: &comp}
	}
	return result
}

func (e *environmentWrapper) Name() string                              { return e.env.Name }
func (e *environmentWrapper) SchemaVersion() string                     { return e.env.SourceVersion }
func (e *environmentWrapper) SourcePath() string                        { return e.env.SourcePath }
func (e *environmentWrapper) Internal() *internal.InternalEnvironment { return e.env }

// componentConfigWrapper wraps an InternalComponentConfig.
type componentConfigWrapper struct {
	c *internal.InternalComponentConfig
}

func (c *componentConfigWrapper) Path() string                            { return c.c.Path }
func (c *componentConfigWrapper) Image() string                           { return c.c.Image }
func (c *componentConfigWrapper) Variables() map[string]interface{}       { return c.c.Variables }
func (c *componentConfigWrapper) Ports() map[string]int                   { return c.c.Ports }
func (c *componentConfigWrapper) Environment() map[string]map[string]string { return c.c.Environment }

func (c *componentConfigWrapper) Scaling() map[string]ScalingConfig {
	result := make(map[string]ScalingConfig)
	for name := range c.c.Scaling {
		s := c.c.Scaling[name]
		result[name] = &scalingConfigWrapper{s: &s}
	}
	return result
}

func (c *componentConfigWrapper) Functions() map[string]FunctionConfig {
	result := make(map[string]FunctionConfig)
	for name := range c.c.Functions {
		f := c.c.Functions[name]
		result[name] = &functionConfigWrapper{f: &f}
	}
	return result
}

func (c *componentConfigWrapper) Routes() map[string]RouteConfig {
	result := make(map[string]RouteConfig)
	for name := range c.c.Routes {
		r := c.c.Routes[name]
		result[name] = &routeConfigWrapper{r: &r}
	}
	return result
}

// scalingConfigWrapper wraps an InternalScalingConfig.
type scalingConfigWrapper struct {
	s *internal.InternalScalingConfig
}

func (s *scalingConfigWrapper) Replicas() int    { return s.s.Replicas }
func (s *scalingConfigWrapper) CPU() string      { return s.s.CPU }
func (s *scalingConfigWrapper) Memory() string   { return s.s.Memory }
func (s *scalingConfigWrapper) MinReplicas() int { return s.s.MinReplicas }
func (s *scalingConfigWrapper) MaxReplicas() int { return s.s.MaxReplicas }

// functionConfigWrapper wraps an InternalFunctionConfig.
type functionConfigWrapper struct {
	f *internal.InternalFunctionConfig
}

func (f *functionConfigWrapper) Regions() []string { return f.f.Regions }
func (f *functionConfigWrapper) Memory() string    { return f.f.Memory }
func (f *functionConfigWrapper) Timeout() int      { return f.f.Timeout }

// routeConfigWrapper wraps an InternalRouteConfig.
type routeConfigWrapper struct {
	r *internal.InternalRouteConfig
}

func (r *routeConfigWrapper) Hostnames() []Hostname {
	result := make([]Hostname, len(r.r.Hostnames))
	for i := range r.r.Hostnames {
		h := r.r.Hostnames[i]
		result[i] = &hostnameWrapper{h: &h}
	}
	return result
}

func (r *routeConfigWrapper) TLS() TLSConfig {
	if r.r.TLS == nil {
		return &tlsConfigWrapper{t: &internal.InternalTLSConfig{}}
	}
	return &tlsConfigWrapper{t: r.r.TLS}
}

// hostnameWrapper wraps an InternalHostname.
type hostnameWrapper struct {
	h *internal.InternalHostname
}

func (h *hostnameWrapper) Subdomain() string { return h.h.Subdomain }
func (h *hostnameWrapper) Host() string      { return h.h.Host }

// tlsConfigWrapper wraps an InternalTLSConfig.
type tlsConfigWrapper struct {
	t *internal.InternalTLSConfig
}

func (t *tlsConfigWrapper) Enabled() bool     { return t.t.Enabled }
func (t *tlsConfigWrapper) SecretName() string { return t.t.SecretName }

// environmentVariableWrapper wraps an InternalEnvironmentVariable.
type environmentVariableWrapper struct {
	v *internal.InternalEnvironmentVariable
}

func (v *environmentVariableWrapper) Name() string            { return v.v.Name }
func (v *environmentVariableWrapper) Description() string     { return v.v.Description }
func (v *environmentVariableWrapper) Default() interface{}    { return v.v.Default }
func (v *environmentVariableWrapper) Required() bool          { return v.v.Required }
func (v *environmentVariableWrapper) Sensitive() bool         { return v.v.Sensitive }
func (v *environmentVariableWrapper) Env() string             { return v.v.Env }
