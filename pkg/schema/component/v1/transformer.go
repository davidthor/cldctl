package v1

import (
	"fmt"
	"strings"

	"github.com/architect-io/arcctl/pkg/schema/component/internal"
)

// Transformer converts v1 schema to internal representation.
type Transformer struct{}

// NewTransformer creates a new v1 transformer.
func NewTransformer() *Transformer {
	return &Transformer{}
}

// Transform converts a v1 schema to the internal representation.
func (t *Transformer) Transform(v1 *SchemaV1) (*internal.InternalComponent, error) {
	ic := &internal.InternalComponent{
		SourceVersion: "v1",
	}

	// Transform builds
	for name, build := range v1.Builds {
		ib := t.transformComponentBuild(name, build)
		ic.Builds = append(ic.Builds, ib)
	}

	// Transform databases
	for name, db := range v1.Databases {
		idb, err := t.transformDatabase(name, db)
		if err != nil {
			return nil, fmt.Errorf("database %s: %w", name, err)
		}
		ic.Databases = append(ic.Databases, idb)
	}

	// Transform buckets
	for name, b := range v1.Buckets {
		ib := t.transformBucket(name, b)
		ic.Buckets = append(ic.Buckets, ib)
	}

	// Transform encryption keys
	for name, ek := range v1.EncryptionKeys {
		iek := t.transformEncryptionKey(name, ek)
		ic.EncryptionKeys = append(ic.EncryptionKeys, iek)
	}

	// Transform SMTP connections
	for name, s := range v1.SMTP {
		is := t.transformSMTP(name, s)
		ic.SMTP = append(ic.SMTP, is)
	}

	// Transform deployments
	for name, dep := range v1.Deployments {
		idep, err := t.transformDeployment(name, dep)
		if err != nil {
			return nil, fmt.Errorf("deployment %s: %w", name, err)
		}
		ic.Deployments = append(ic.Deployments, idep)
	}

	// Transform functions
	for name, fn := range v1.Functions {
		ifn, err := t.transformFunction(name, fn)
		if err != nil {
			return nil, fmt.Errorf("function %s: %w", name, err)
		}
		ic.Functions = append(ic.Functions, ifn)
	}

	// Transform services
	for name, svc := range v1.Services {
		isvc := t.transformService(name, svc)
		ic.Services = append(ic.Services, isvc)
	}

	// Transform routes
	for name, rt := range v1.Routes {
		irt, err := t.transformRoute(name, rt)
		if err != nil {
			return nil, fmt.Errorf("route %s: %w", name, err)
		}
		ic.Routes = append(ic.Routes, irt)
	}

	// Transform cronjobs
	for name, cj := range v1.Cronjobs {
		icj, err := t.transformCronjob(name, cj)
		if err != nil {
			return nil, fmt.Errorf("cronjob %s: %w", name, err)
		}
		ic.Cronjobs = append(ic.Cronjobs, icj)
	}

	// Transform observability
	if v1.Observability != nil {
		ic.Observability = t.transformObservability(v1.Observability)
	}

	// Transform variables
	for name, v := range v1.Variables {
		iv := t.transformVariable(name, v)
		ic.Variables = append(ic.Variables, iv)
	}

	// Transform dependencies
	for name, component := range v1.Dependencies {
		id := t.transformDependency(name, component)
		ic.Dependencies = append(ic.Dependencies, id)
	}

	// Transform outputs
	for name, o := range v1.Outputs {
		io := t.transformOutput(name, o)
		ic.Outputs = append(ic.Outputs, io)
	}

	return ic, nil
}

func (t *Transformer) transformDatabase(name string, db DatabaseV1) (internal.InternalDatabase, error) {
	dbType, version := parseTypeVersion(db.Type)

	idb := internal.InternalDatabase{
		Name:    name,
		Type:    dbType,
		Version: version,
	}

	if db.Migrations != nil {
		idb.Migrations = &internal.InternalMigrations{
			Image:       db.Migrations.Image,
			Command:     db.Migrations.Command,
			Environment: db.Migrations.Environment,
		}

		if db.Migrations.Build != nil {
			idb.Migrations.Build = t.transformBuild(db.Migrations.Build)
		}
	}

	return idb, nil
}

func (t *Transformer) transformBucket(name string, b BucketV1) internal.InternalBucket {
	return internal.InternalBucket{
		Name:       name,
		Type:       b.Type,
		Versioning: b.Versioning,
		Public:     b.Public,
	}
}

func (t *Transformer) transformEncryptionKey(name string, ek EncryptionKeyV1) internal.InternalEncryptionKey {
	iek := internal.InternalEncryptionKey{
		Name:  name,
		Type:  ek.Type,
		Bits:  ek.Bits,
		Curve: ek.Curve,
		Bytes: ek.Bytes,
	}

	// Apply defaults based on key type
	switch ek.Type {
	case "rsa":
		if iek.Bits == 0 {
			iek.Bits = 2048
		}
	case "ecdsa":
		if iek.Curve == "" {
			iek.Curve = "P-256"
		}
	case "symmetric":
		if iek.Bytes == 0 {
			iek.Bytes = 32
		}
	}

	return iek
}

func (t *Transformer) transformSMTP(name string, s SMTPV1) internal.InternalSMTP {
	return internal.InternalSMTP{
		Name:        name,
		Description: s.Description,
	}
}

func (t *Transformer) transformDeployment(name string, dep DeploymentV1) (internal.InternalDeployment, error) {
	idep := internal.InternalDeployment{
		Name:             name,
		Image:            dep.Image,
		Command:          dep.Command,
		Entrypoint:       dep.Entrypoint,
		WorkingDirectory: dep.WorkingDirectory,
		CPU:              dep.CPU,
		Memory:           dep.Memory,
		Replicas:         defaultInt(dep.Replicas, 1),
		Labels:           dep.Labels,
	}

	// Transform runtime
	if dep.Runtime != nil {
		idep.Runtime = t.transformRuntime(dep.Runtime)
	}

	// Transform environment with expression detection
	idep.Environment = make(map[string]internal.Expression)
	for k, v := range dep.Environment {
		idep.Environment[k] = internal.NewExpression(v)
	}

	// Transform volumes
	for _, vol := range dep.Volumes {
		idep.Volumes = append(idep.Volumes, internal.InternalVolume{
			MountPath: vol.MountPath,
			HostPath:  vol.HostPath,
			Name:      vol.Name,
			ReadOnly:  vol.ReadOnly,
		})
	}

	// Transform probes
	if dep.LivenessProbe != nil {
		idep.LivenessProbe = t.transformProbe(dep.LivenessProbe)
	}
	if dep.ReadinessProbe != nil {
		idep.ReadinessProbe = t.transformProbe(dep.ReadinessProbe)
	}

	return idep, nil
}

func (t *Transformer) transformFunction(name string, fn FunctionV1) (internal.InternalFunction, error) {
	ifn := internal.InternalFunction{
		Name:    name,
		Port:    fn.Port,
		CPU:     fn.CPU,
		Memory:  fn.Memory,
		Timeout: fn.Timeout,
	}

	// Transform discriminated union
	if fn.Src != nil {
		ifn.Src = &internal.InternalFunctionSource{
			Path:      fn.Src.Path,
			Language:  fn.Src.Language,
			Runtime:   fn.Src.Runtime,
			Framework: fn.Src.Framework,
			Install:   fn.Src.Install,
			Dev:       fn.Src.Dev,
			Build:     fn.Src.Build,
			Start:     fn.Src.Start,
			Handler:   fn.Src.Handler,
			Entry:     fn.Src.Entry,
		}
	}

	if fn.Container != nil {
		ifn.Container = &internal.InternalFunctionContainer{
			Image: fn.Container.Image,
		}
		if fn.Container.Build != nil {
			ifn.Container.Build = t.transformBuild(fn.Container.Build)
		}
	}

	// Transform environment with expression detection
	ifn.Environment = make(map[string]internal.Expression)
	for k, v := range fn.Environment {
		ifn.Environment[k] = internal.NewExpression(v)
	}

	return ifn, nil
}

func (t *Transformer) transformService(name string, svc ServiceV1) internal.InternalService {
	return internal.InternalService{
		Name:       name,
		Deployment: svc.Deployment,
		URL:        svc.URL,
		Port:       svc.Port,
		Protocol:   defaultString(svc.Protocol, "http"),
	}
}

func (t *Transformer) transformRoute(name string, rt RouteV1) (internal.InternalRoute, error) {
	irt := internal.InternalRoute{
		Name:     name,
		Type:     rt.Type,
		Internal: rt.Internal,
		Service:  rt.Service,
		Function: rt.Function,
		Port:     rt.Port,
	}

	// Transform rules
	for _, rule := range rt.Rules {
		irule, err := t.transformRouteRule(rule)
		if err != nil {
			return irt, err
		}
		irt.Rules = append(irt.Rules, irule)
	}

	return irt, nil
}

func (t *Transformer) transformRouteRule(rule RouteRuleV1) (internal.InternalRouteRule, error) {
	irule := internal.InternalRouteRule{
		Name: rule.Name,
	}

	// Transform matches
	for _, match := range rule.Matches {
		imatch := internal.InternalRouteMatch{
			Method: match.Method,
		}

		if match.Path != nil {
			imatch.Path = &internal.InternalPathMatch{
				Type:  match.Path.Type,
				Value: match.Path.Value,
			}
		}

		for _, h := range match.Headers {
			imatch.Headers = append(imatch.Headers, internal.InternalHeaderMatch{
				Name:  h.Name,
				Type:  defaultString(h.Type, "Exact"),
				Value: h.Value,
			})
		}

		for _, q := range match.QueryParams {
			imatch.QueryParams = append(imatch.QueryParams, internal.InternalQueryParamMatch{
				Name:  q.Name,
				Type:  defaultString(q.Type, "Exact"),
				Value: q.Value,
			})
		}

		if match.GRPCMethod != nil {
			imatch.GRPCMethod = &internal.InternalGRPCMethodMatch{
				Service: match.GRPCMethod.Service,
				Method:  match.GRPCMethod.Method,
			}
		}

		irule.Matches = append(irule.Matches, imatch)
	}

	// Transform backend refs
	for _, ref := range rule.BackendRefs {
		irule.BackendRefs = append(irule.BackendRefs, internal.InternalBackendRef{
			Service:  ref.Service,
			Function: ref.Function,
			Port:     ref.Port,
			Weight:   defaultInt(ref.Weight, 1),
		})
	}

	// Transform filters
	for _, filter := range rule.Filters {
		ifilter := internal.InternalRouteFilter{
			Type: filter.Type,
		}

		if filter.RequestHeaderModifier != nil {
			ifilter.RequestHeaderModifier = t.transformHeaderModifier(filter.RequestHeaderModifier)
		}
		if filter.ResponseHeaderModifier != nil {
			ifilter.ResponseHeaderModifier = t.transformHeaderModifier(filter.ResponseHeaderModifier)
		}
		if filter.RequestRedirect != nil {
			ifilter.RequestRedirect = &internal.InternalRedirect{
				Scheme:     filter.RequestRedirect.Scheme,
				Hostname:   filter.RequestRedirect.Hostname,
				Port:       filter.RequestRedirect.Port,
				StatusCode: filter.RequestRedirect.StatusCode,
			}
		}
		if filter.URLRewrite != nil {
			ifilter.URLRewrite = &internal.InternalURLRewrite{
				Hostname: filter.URLRewrite.Hostname,
			}
			if filter.URLRewrite.Path != nil {
				ifilter.URLRewrite.Path = &internal.InternalPathModifier{
					Type:               filter.URLRewrite.Path.Type,
					ReplaceFullPath:    filter.URLRewrite.Path.ReplaceFullPath,
					ReplacePrefixMatch: filter.URLRewrite.Path.ReplacePrefixMatch,
				}
			}
		}
		if filter.RequestMirror != nil {
			ifilter.RequestMirror = &internal.InternalMirror{
				Service: filter.RequestMirror.Service,
				Port:    filter.RequestMirror.Port,
			}
		}

		irule.Filters = append(irule.Filters, ifilter)
	}

	// Transform timeouts
	if rule.Timeouts != nil {
		irule.Timeouts = &internal.InternalTimeouts{
			Request:        rule.Timeouts.Request,
			BackendRequest: rule.Timeouts.BackendRequest,
		}
	}

	return irule, nil
}

func (t *Transformer) transformHeaderModifier(hm *HeaderModifierV1) *internal.InternalHeaderModifier {
	ihm := &internal.InternalHeaderModifier{
		Remove: hm.Remove,
	}

	for _, h := range hm.Add {
		ihm.Add = append(ihm.Add, internal.InternalHeaderValue{
			Name:  h.Name,
			Value: h.Value,
		})
	}
	for _, h := range hm.Set {
		ihm.Set = append(ihm.Set, internal.InternalHeaderValue{
			Name:  h.Name,
			Value: h.Value,
		})
	}

	return ihm
}

func (t *Transformer) transformCronjob(name string, cj CronjobV1) (internal.InternalCronjob, error) {
	icj := internal.InternalCronjob{
		Name:     name,
		Image:    cj.Image,
		Schedule: cj.Schedule,
		Command:  cj.Command,
		CPU:      cj.CPU,
		Memory:   cj.Memory,
	}

	if cj.Build != nil {
		icj.Build = t.transformBuild(cj.Build)
	}

	// Transform environment with expression detection
	icj.Environment = make(map[string]internal.Expression)
	for k, v := range cj.Environment {
		icj.Environment[k] = internal.NewExpression(v)
	}

	return icj, nil
}

func (t *Transformer) transformVariable(name string, v VariableV1) internal.InternalVariable {
	return internal.InternalVariable{
		Name:        name,
		Description: v.Description,
		Default:     v.Default,
		Required:    v.Required,
		Sensitive:   v.Sensitive || v.Secret,
	}
}

func (t *Transformer) transformDependency(name string, component string) internal.InternalDependency {
	return internal.InternalDependency{
		Name:      name,
		Component: component,
	}
}

func (t *Transformer) transformOutput(name string, o OutputV1) internal.InternalOutput {
	return internal.InternalOutput{
		Name:        name,
		Description: o.Description,
		Value:       internal.NewExpression(o.Value),
		Sensitive:   o.Sensitive,
	}
}

func (t *Transformer) transformComponentBuild(name string, b BuildV1) internal.InternalComponentBuild {
	return internal.InternalComponentBuild{
		Name:       name,
		Context:    b.Context,
		Dockerfile: defaultString(b.Dockerfile, "Dockerfile"),
		Target:     b.Target,
		Args:       b.Args,
	}
}

func (t *Transformer) transformBuild(b *BuildV1) *internal.InternalBuild {
	return &internal.InternalBuild{
		Context:    b.Context,
		Dockerfile: defaultString(b.Dockerfile, "Dockerfile"),
		Target:     b.Target,
		Args:       b.Args,
	}
}

func (t *Transformer) transformProbe(p *ProbeV1) *internal.InternalProbe {
	return &internal.InternalProbe{
		Path:                p.Path,
		Port:                p.Port,
		Command:             p.Command,
		TCPPort:             p.TCPPort,
		InitialDelaySeconds: p.InitialDelaySeconds,
		PeriodSeconds:       p.PeriodSeconds,
		TimeoutSeconds:      p.TimeoutSeconds,
		SuccessThreshold:    p.SuccessThreshold,
		FailureThreshold:    p.FailureThreshold,
	}
}

func (t *Transformer) transformRuntime(rt *RuntimeV1) *internal.InternalRuntime {
	return &internal.InternalRuntime{
		Language: rt.Language,
		OS:       rt.OS,
		Arch:     rt.Arch,
		Packages: rt.Packages,
		Setup:    rt.Setup,
	}
}

func (t *Transformer) transformObservability(obs *ObservabilityV1) *internal.InternalObservability {
	if !obs.Enabled {
		return nil
	}

	return &internal.InternalObservability{
		Inject:     defaultBoolPtr(obs.Inject, false),
		Logs:       defaultBoolPtr(obs.Logs, true),
		Traces:     defaultBoolPtr(obs.Traces, true),
		Metrics:    defaultBoolPtr(obs.Metrics, true),
		Attributes: obs.Attributes,
	}
}

func defaultBoolPtr(val *bool, def bool) bool {
	if val == nil {
		return def
	}
	return *val
}

// parseTypeVersion parses "postgres:^15" into ("postgres", "^15")
func parseTypeVersion(typeSpec string) (string, string) {
	parts := strings.SplitN(typeSpec, ":", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func defaultString(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

func defaultInt(val, def int) int {
	if val == 0 {
		return def
	}
	return val
}
