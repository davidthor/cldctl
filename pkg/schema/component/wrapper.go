package component

import (
	"encoding/json"

	"github.com/architect-io/arcctl/pkg/schema/component/internal"
	"gopkg.in/yaml.v3"
)

// componentWrapper wraps the internal component to implement the Component interface.
type componentWrapper struct {
	ic *internal.InternalComponent
}

func newComponentWrapper(ic *internal.InternalComponent) *componentWrapper {
	return &componentWrapper{ic: ic}
}

func (c *componentWrapper) Readme() string        { return c.ic.Readme }
func (c *componentWrapper) SchemaVersion() string { return c.ic.SourceVersion }
func (c *componentWrapper) SourcePath() string   { return c.ic.SourcePath }
func (c *componentWrapper) Internal() *internal.InternalComponent { return c.ic }

func (c *componentWrapper) Builds() []ComponentBuild {
	result := make([]ComponentBuild, len(c.ic.Builds))
	for i := range c.ic.Builds {
		result[i] = &componentBuildWrapper{b: &c.ic.Builds[i]}
	}
	return result
}

func (c *componentWrapper) Databases() []Database {
	result := make([]Database, len(c.ic.Databases))
	for i := range c.ic.Databases {
		result[i] = &databaseWrapper{db: &c.ic.Databases[i]}
	}
	return result
}

func (c *componentWrapper) Buckets() []Bucket {
	result := make([]Bucket, len(c.ic.Buckets))
	for i := range c.ic.Buckets {
		result[i] = &bucketWrapper{b: &c.ic.Buckets[i]}
	}
	return result
}

func (c *componentWrapper) EncryptionKeys() []EncryptionKey {
	result := make([]EncryptionKey, len(c.ic.EncryptionKeys))
	for i := range c.ic.EncryptionKeys {
		result[i] = &encryptionKeyWrapper{ek: &c.ic.EncryptionKeys[i]}
	}
	return result
}

func (c *componentWrapper) SMTP() []SMTPConnection {
	result := make([]SMTPConnection, len(c.ic.SMTP))
	for i := range c.ic.SMTP {
		result[i] = &smtpWrapper{s: &c.ic.SMTP[i]}
	}
	return result
}

func (c *componentWrapper) Deployments() []Deployment {
	result := make([]Deployment, len(c.ic.Deployments))
	for i := range c.ic.Deployments {
		result[i] = &deploymentWrapper{dep: &c.ic.Deployments[i]}
	}
	return result
}

func (c *componentWrapper) Functions() []Function {
	result := make([]Function, len(c.ic.Functions))
	for i := range c.ic.Functions {
		result[i] = &functionWrapper{fn: &c.ic.Functions[i]}
	}
	return result
}

func (c *componentWrapper) Services() []Service {
	result := make([]Service, len(c.ic.Services))
	for i := range c.ic.Services {
		result[i] = &serviceWrapper{svc: &c.ic.Services[i]}
	}
	return result
}

func (c *componentWrapper) Routes() []Route {
	result := make([]Route, len(c.ic.Routes))
	for i := range c.ic.Routes {
		result[i] = &routeWrapper{rt: &c.ic.Routes[i]}
	}
	return result
}

func (c *componentWrapper) Cronjobs() []Cronjob {
	result := make([]Cronjob, len(c.ic.Cronjobs))
	for i := range c.ic.Cronjobs {
		result[i] = &cronjobWrapper{cj: &c.ic.Cronjobs[i]}
	}
	return result
}

func (c *componentWrapper) Variables() []Variable {
	result := make([]Variable, len(c.ic.Variables))
	for i := range c.ic.Variables {
		result[i] = &variableWrapper{v: &c.ic.Variables[i]}
	}
	return result
}

func (c *componentWrapper) Dependencies() []Dependency {
	result := make([]Dependency, len(c.ic.Dependencies))
	for i := range c.ic.Dependencies {
		result[i] = &dependencyWrapper{d: &c.ic.Dependencies[i]}
	}
	return result
}

func (c *componentWrapper) Outputs() []Output {
	result := make([]Output, len(c.ic.Outputs))
	for i := range c.ic.Outputs {
		result[i] = &outputWrapper{o: &c.ic.Outputs[i]}
	}
	return result
}

func (c *componentWrapper) ToYAML() ([]byte, error) {
	return yaml.Marshal(c.ic)
}

func (c *componentWrapper) ToJSON() ([]byte, error) {
	return json.Marshal(c.ic)
}

// Database wrapper
type databaseWrapper struct {
	db *internal.InternalDatabase
}

func (d *databaseWrapper) Name() string    { return d.db.Name }
func (d *databaseWrapper) Type() string    { return d.db.Type }
func (d *databaseWrapper) Version() string { return d.db.Version }

func (d *databaseWrapper) Migrations() Migrations {
	if d.db.Migrations == nil {
		return nil
	}
	return &migrationsWrapper{m: d.db.Migrations}
}

// Migrations wrapper
type migrationsWrapper struct {
	m *internal.InternalMigrations
}

func (m *migrationsWrapper) Image() string               { return m.m.Image }
func (m *migrationsWrapper) Command() []string           { return m.m.Command }
func (m *migrationsWrapper) Environment() map[string]string { return m.m.Environment }

func (m *migrationsWrapper) Build() Build {
	if m.m.Build == nil {
		return nil
	}
	return &buildWrapper{b: m.m.Build}
}

// Build wrapper
type buildWrapper struct {
	b *internal.InternalBuild
}

func (b *buildWrapper) Context() string            { return b.b.Context }
func (b *buildWrapper) Dockerfile() string         { return b.b.Dockerfile }
func (b *buildWrapper) Target() string             { return b.b.Target }
func (b *buildWrapper) Args() map[string]string    { return b.b.Args }

// ComponentBuild wrapper
type componentBuildWrapper struct {
	b *internal.InternalComponentBuild
}

func (cb *componentBuildWrapper) Name() string             { return cb.b.Name }
func (cb *componentBuildWrapper) Context() string          { return cb.b.Context }
func (cb *componentBuildWrapper) Dockerfile() string       { return cb.b.Dockerfile }
func (cb *componentBuildWrapper) Target() string           { return cb.b.Target }
func (cb *componentBuildWrapper) Args() map[string]string  { return cb.b.Args }

// Bucket wrapper
type bucketWrapper struct {
	b *internal.InternalBucket
}

func (b *bucketWrapper) Name() string       { return b.b.Name }
func (b *bucketWrapper) Type() string       { return b.b.Type }
func (b *bucketWrapper) Versioning() bool   { return b.b.Versioning }
func (b *bucketWrapper) Public() bool       { return b.b.Public }

// EncryptionKey wrapper
type encryptionKeyWrapper struct {
	ek *internal.InternalEncryptionKey
}

func (e *encryptionKeyWrapper) Name() string  { return e.ek.Name }
func (e *encryptionKeyWrapper) Type() string  { return e.ek.Type }
func (e *encryptionKeyWrapper) Bits() int     { return e.ek.Bits }
func (e *encryptionKeyWrapper) Curve() string { return e.ek.Curve }
func (e *encryptionKeyWrapper) Bytes() int    { return e.ek.Bytes }

// SMTP wrapper
type smtpWrapper struct {
	s *internal.InternalSMTP
}

func (s *smtpWrapper) Name() string        { return s.s.Name }
func (s *smtpWrapper) Description() string { return s.s.Description }

// Deployment wrapper
type deploymentWrapper struct {
	dep *internal.InternalDeployment
}

func (d *deploymentWrapper) Name() string             { return d.dep.Name }
func (d *deploymentWrapper) Image() string            { return d.dep.Image }
func (d *deploymentWrapper) Command() []string        { return d.dep.Command }
func (d *deploymentWrapper) Entrypoint() []string     { return d.dep.Entrypoint }
func (d *deploymentWrapper) WorkingDirectory() string { return d.dep.WorkingDirectory }
func (d *deploymentWrapper) CPU() string              { return d.dep.CPU }
func (d *deploymentWrapper) Memory() string           { return d.dep.Memory }
func (d *deploymentWrapper) Replicas() int            { return d.dep.Replicas }

func (d *deploymentWrapper) Runtime() Runtime {
	if d.dep.Runtime == nil {
		return nil
	}
	return &runtimeWrapper{rt: d.dep.Runtime}
}

// Runtime wrapper
type runtimeWrapper struct {
	rt *internal.InternalRuntime
}

func (r *runtimeWrapper) Language() string   { return r.rt.Language }
func (r *runtimeWrapper) OS() string         { return r.rt.OS }
func (r *runtimeWrapper) Arch() string       { return r.rt.Arch }
func (r *runtimeWrapper) Packages() []string { return r.rt.Packages }
func (r *runtimeWrapper) Setup() []string    { return r.rt.Setup }

func (d *deploymentWrapper) Environment() map[string]string {
	result := make(map[string]string)
	for k, v := range d.dep.Environment {
		result[k] = v.Raw
	}
	return result
}

func (d *deploymentWrapper) Volumes() []Volume {
	result := make([]Volume, len(d.dep.Volumes))
	for i := range d.dep.Volumes {
		result[i] = &volumeWrapper{v: &d.dep.Volumes[i]}
	}
	return result
}

func (d *deploymentWrapper) LivenessProbe() Probe {
	if d.dep.LivenessProbe == nil {
		return nil
	}
	return &probeWrapper{p: d.dep.LivenessProbe}
}

func (d *deploymentWrapper) ReadinessProbe() Probe {
	if d.dep.ReadinessProbe == nil {
		return nil
	}
	return &probeWrapper{p: d.dep.ReadinessProbe}
}

// Function wrapper
type functionWrapper struct {
	fn *internal.InternalFunction
}

func (f *functionWrapper) Name() string   { return f.fn.Name }
func (f *functionWrapper) Port() int      { return f.fn.Port }
func (f *functionWrapper) CPU() string    { return f.fn.CPU }
func (f *functionWrapper) Memory() string { return f.fn.Memory }
func (f *functionWrapper) Timeout() int   { return f.fn.Timeout }

func (f *functionWrapper) Src() FunctionSource {
	if f.fn.Src == nil {
		return nil
	}
	return &functionSourceWrapper{src: f.fn.Src}
}

func (f *functionWrapper) Container() FunctionContainer {
	if f.fn.Container == nil {
		return nil
	}
	return &functionContainerWrapper{c: f.fn.Container}
}

func (f *functionWrapper) IsSourceBased() bool    { return f.fn.Src != nil }
func (f *functionWrapper) IsContainerBased() bool { return f.fn.Container != nil }

func (f *functionWrapper) Environment() map[string]string {
	result := make(map[string]string)
	for k, v := range f.fn.Environment {
		result[k] = v.Raw
	}
	return result
}

// FunctionSource wrapper
type functionSourceWrapper struct {
	src *internal.InternalFunctionSource
}

func (f *functionSourceWrapper) Path() string      { return f.src.Path }
func (f *functionSourceWrapper) Language() string  { return f.src.Language }
func (f *functionSourceWrapper) Runtime() string   { return f.src.Runtime }
func (f *functionSourceWrapper) Framework() string { return f.src.Framework }
func (f *functionSourceWrapper) Install() string   { return f.src.Install }
func (f *functionSourceWrapper) Dev() string       { return f.src.Dev }
func (f *functionSourceWrapper) Build() string     { return f.src.Build }
func (f *functionSourceWrapper) Start() string     { return f.src.Start }
func (f *functionSourceWrapper) Handler() string   { return f.src.Handler }
func (f *functionSourceWrapper) Entry() string     { return f.src.Entry }

// FunctionContainer wrapper
type functionContainerWrapper struct {
	c *internal.InternalFunctionContainer
}

func (f *functionContainerWrapper) Image() string { return f.c.Image }

func (f *functionContainerWrapper) Build() Build {
	if f.c.Build == nil {
		return nil
	}
	return &buildWrapper{b: f.c.Build}
}

// Service wrapper
type serviceWrapper struct {
	svc *internal.InternalService
}

func (s *serviceWrapper) Name() string       { return s.svc.Name }
func (s *serviceWrapper) Deployment() string { return s.svc.Deployment }
func (s *serviceWrapper) URL() string        { return s.svc.URL }
func (s *serviceWrapper) Port() int          { return s.svc.Port }
func (s *serviceWrapper) Protocol() string   { return s.svc.Protocol }

// Route wrapper
type routeWrapper struct {
	rt *internal.InternalRoute
}

func (r *routeWrapper) Name() string     { return r.rt.Name }
func (r *routeWrapper) Type() string     { return r.rt.Type }
func (r *routeWrapper) Internal() bool   { return r.rt.Internal }
func (r *routeWrapper) Service() string  { return r.rt.Service }
func (r *routeWrapper) Function() string { return r.rt.Function }
func (r *routeWrapper) Port() int        { return r.rt.Port }

func (r *routeWrapper) Rules() []RouteRule {
	result := make([]RouteRule, len(r.rt.Rules))
	for i := range r.rt.Rules {
		result[i] = &routeRuleWrapper{rule: &r.rt.Rules[i]}
	}
	return result
}

// RouteRule wrapper
type routeRuleWrapper struct {
	rule *internal.InternalRouteRule
}

func (r *routeRuleWrapper) Name() string { return r.rule.Name }

func (r *routeRuleWrapper) Matches() []RouteMatch {
	result := make([]RouteMatch, len(r.rule.Matches))
	for i := range r.rule.Matches {
		result[i] = &routeMatchWrapper{match: &r.rule.Matches[i]}
	}
	return result
}

func (r *routeRuleWrapper) BackendRefs() []BackendRef {
	result := make([]BackendRef, len(r.rule.BackendRefs))
	for i := range r.rule.BackendRefs {
		result[i] = &backendRefWrapper{ref: &r.rule.BackendRefs[i]}
	}
	return result
}

func (r *routeRuleWrapper) Filters() []RouteFilter {
	result := make([]RouteFilter, len(r.rule.Filters))
	for i := range r.rule.Filters {
		result[i] = &routeFilterWrapper{filter: &r.rule.Filters[i]}
	}
	return result
}

func (r *routeRuleWrapper) Timeouts() Timeouts {
	if r.rule.Timeouts == nil {
		return nil
	}
	return &timeoutsWrapper{t: r.rule.Timeouts}
}

// RouteMatch wrapper
type routeMatchWrapper struct {
	match *internal.InternalRouteMatch
}

func (r *routeMatchWrapper) Method() string { return r.match.Method }

func (r *routeMatchWrapper) Path() PathMatch {
	if r.match.Path == nil {
		return nil
	}
	return &pathMatchWrapper{p: r.match.Path}
}

func (r *routeMatchWrapper) Headers() []HeaderMatch {
	result := make([]HeaderMatch, len(r.match.Headers))
	for i := range r.match.Headers {
		result[i] = &headerMatchWrapper{h: &r.match.Headers[i]}
	}
	return result
}

func (r *routeMatchWrapper) QueryParams() []QueryParamMatch {
	result := make([]QueryParamMatch, len(r.match.QueryParams))
	for i := range r.match.QueryParams {
		result[i] = &queryParamMatchWrapper{q: &r.match.QueryParams[i]}
	}
	return result
}

func (r *routeMatchWrapper) GRPCMethod() GRPCMethodMatch {
	if r.match.GRPCMethod == nil {
		return nil
	}
	return &grpcMethodMatchWrapper{m: r.match.GRPCMethod}
}

// PathMatch wrapper
type pathMatchWrapper struct {
	p *internal.InternalPathMatch
}

func (p *pathMatchWrapper) Type() string  { return p.p.Type }
func (p *pathMatchWrapper) Value() string { return p.p.Value }

// HeaderMatch wrapper
type headerMatchWrapper struct {
	h *internal.InternalHeaderMatch
}

func (h *headerMatchWrapper) Name() string  { return h.h.Name }
func (h *headerMatchWrapper) Type() string  { return h.h.Type }
func (h *headerMatchWrapper) Value() string { return h.h.Value }

// QueryParamMatch wrapper
type queryParamMatchWrapper struct {
	q *internal.InternalQueryParamMatch
}

func (q *queryParamMatchWrapper) Name() string  { return q.q.Name }
func (q *queryParamMatchWrapper) Type() string  { return q.q.Type }
func (q *queryParamMatchWrapper) Value() string { return q.q.Value }

// GRPCMethodMatch wrapper
type grpcMethodMatchWrapper struct {
	m *internal.InternalGRPCMethodMatch
}

func (g *grpcMethodMatchWrapper) Service() string { return g.m.Service }
func (g *grpcMethodMatchWrapper) Method() string  { return g.m.Method }

// BackendRef wrapper
type backendRefWrapper struct {
	ref *internal.InternalBackendRef
}

func (b *backendRefWrapper) Service() string  { return b.ref.Service }
func (b *backendRefWrapper) Function() string { return b.ref.Function }
func (b *backendRefWrapper) Port() int        { return b.ref.Port }
func (b *backendRefWrapper) Weight() int      { return b.ref.Weight }

// RouteFilter wrapper
type routeFilterWrapper struct {
	filter *internal.InternalRouteFilter
}

func (r *routeFilterWrapper) Type() string { return r.filter.Type }

// Timeouts wrapper
type timeoutsWrapper struct {
	t *internal.InternalTimeouts
}

func (t *timeoutsWrapper) Request() string        { return t.t.Request }
func (t *timeoutsWrapper) BackendRequest() string { return t.t.BackendRequest }

// Cronjob wrapper
type cronjobWrapper struct {
	cj *internal.InternalCronjob
}

func (c *cronjobWrapper) Name() string      { return c.cj.Name }
func (c *cronjobWrapper) Image() string     { return c.cj.Image }
func (c *cronjobWrapper) Schedule() string  { return c.cj.Schedule }
func (c *cronjobWrapper) Command() []string { return c.cj.Command }
func (c *cronjobWrapper) CPU() string       { return c.cj.CPU }
func (c *cronjobWrapper) Memory() string    { return c.cj.Memory }

func (c *cronjobWrapper) Build() Build {
	if c.cj.Build == nil {
		return nil
	}
	return &buildWrapper{b: c.cj.Build}
}

func (c *cronjobWrapper) Environment() map[string]string {
	result := make(map[string]string)
	for k, v := range c.cj.Environment {
		result[k] = v.Raw
	}
	return result
}

// Variable wrapper
type variableWrapper struct {
	v *internal.InternalVariable
}

func (v *variableWrapper) Name() string          { return v.v.Name }
func (v *variableWrapper) Description() string   { return v.v.Description }
func (v *variableWrapper) Default() interface{}  { return v.v.Default }
func (v *variableWrapper) Required() bool        { return v.v.Required }
func (v *variableWrapper) Sensitive() bool       { return v.v.Sensitive }

// Dependency wrapper
type dependencyWrapper struct {
	d *internal.InternalDependency
}

func (d *dependencyWrapper) Name() string      { return d.d.Name }
func (d *dependencyWrapper) Component() string { return d.d.Component }

// Output wrapper
type outputWrapper struct {
	o *internal.InternalOutput
}

func (o *outputWrapper) Name() string        { return o.o.Name }
func (o *outputWrapper) Description() string { return o.o.Description }
func (o *outputWrapper) Value() string       { return o.o.Value.Raw }
func (o *outputWrapper) Sensitive() bool     { return o.o.Sensitive }

// Volume wrapper
type volumeWrapper struct {
	v *internal.InternalVolume
}

func (v *volumeWrapper) MountPath() string { return v.v.MountPath }
func (v *volumeWrapper) HostPath() string  { return v.v.HostPath }
func (v *volumeWrapper) Name() string      { return v.v.Name }
func (v *volumeWrapper) ReadOnly() bool    { return v.v.ReadOnly }

// Probe wrapper
type probeWrapper struct {
	p *internal.InternalProbe
}

func (p *probeWrapper) Path() string                 { return p.p.Path }
func (p *probeWrapper) Port() int                    { return p.p.Port }
func (p *probeWrapper) Command() []string            { return p.p.Command }
func (p *probeWrapper) TCPPort() int                 { return p.p.TCPPort }
func (p *probeWrapper) InitialDelaySeconds() int     { return p.p.InitialDelaySeconds }
func (p *probeWrapper) PeriodSeconds() int           { return p.p.PeriodSeconds }
func (p *probeWrapper) TimeoutSeconds() int          { return p.p.TimeoutSeconds }
func (p *probeWrapper) SuccessThreshold() int        { return p.p.SuccessThreshold }
func (p *probeWrapper) FailureThreshold() int        { return p.p.FailureThreshold }
