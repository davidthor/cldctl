package expression

import (
	"fmt"
	"strings"
)

// EvalContext provides values for expression evaluation.
type EvalContext struct {
	Builds         map[string]BuildOutputs
	Databases      map[string]DatabaseOutputs
	Buckets        map[string]BucketOutputs
	EncryptionKeys map[string]EncryptionKeyOutputs
	SMTP           map[string]SMTPOutputs
	Ports          map[string]PortOutputs
	Services       map[string]ServiceOutputs
	Routes         map[string]RouteOutputs
	Functions      map[string]FunctionOutputs
	Observability  *ObservabilityOutputs
	Variables      map[string]interface{}
	Dependencies   map[string]DependencyOutputs
	Dependents     map[string]DependentOutputs
}

// BuildOutputs contains outputs from a completed Docker build.
type BuildOutputs struct {
	Image string // The built image tag/ID
}

// DatabaseOutputs contains outputs from a provisioned database.
type DatabaseOutputs struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	URL      string

	// Optional read/write endpoint separation.
	// When nil, read/write expressions fall back to top-level values.
	Read  *DatabaseEndpoint
	Write *DatabaseEndpoint
}

// DatabaseEndpoint contains connection information for a specific endpoint direction (read or write).
type DatabaseEndpoint struct {
	Host     string
	Port     int
	Username string
	Password string
	URL      string
}

// BucketOutputs contains outputs from a provisioned bucket.
type BucketOutputs struct {
	Endpoint        string
	Bucket          string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
}

// EncryptionKeyOutputs contains outputs from a provisioned encryption key.
type EncryptionKeyOutputs struct {
	// Asymmetric key outputs (RSA/ECDSA)
	PrivateKey       string
	PublicKey        string
	PrivateKeyBase64 string
	PublicKeyBase64  string

	// Symmetric key outputs
	Key       string // Hex-encoded
	KeyBase64 string // Base64-encoded
}

// SMTPOutputs contains outputs from a provisioned SMTP connection.
type SMTPOutputs struct {
	Host     string
	Port     int
	Username string
	Password string
}

// PortOutputs contains outputs from a provisioned port allocation.
type PortOutputs struct {
	Port int // The allocated port number
}

// ServiceOutputs contains outputs from a provisioned service.
type ServiceOutputs struct {
	URL      string
	Host     string
	Port     int
	Protocol string
}

// RouteOutputs contains outputs from a provisioned route.
type RouteOutputs struct {
	URL   string
	Hosts []string
}

// FunctionOutputs contains outputs from a provisioned function.
type FunctionOutputs struct {
	URL string
	ID  string
}

// ObservabilityOutputs contains outputs from the observability hook.
type ObservabilityOutputs struct {
	Endpoint   string // OTel collector endpoint (e.g., http://otel-collector:4318)
	Protocol   string // OTLP protocol (e.g., http/protobuf, grpc)
	Attributes string // Merged resource attributes as comma-separated key=value pairs
}

// DependencyOutputs contains outputs from a dependency component.
type DependencyOutputs struct {
	Services map[string]ServiceOutputs
	Routes   map[string]RouteOutputs
	Outputs  map[string]interface{} // Custom outputs defined by the component
}

// DependentOutputs contains outputs from a dependent component.
type DependentOutputs struct {
	Services map[string]ServiceOutputs
	Routes   map[string]RouteOutputs
}

// NewEvalContext creates a new empty evaluation context.
func NewEvalContext() *EvalContext {
	return &EvalContext{
		Builds:         make(map[string]BuildOutputs),
		Databases:      make(map[string]DatabaseOutputs),
		Buckets:        make(map[string]BucketOutputs),
		EncryptionKeys: make(map[string]EncryptionKeyOutputs),
		SMTP:           make(map[string]SMTPOutputs),
		Ports:          make(map[string]PortOutputs),
		Services:       make(map[string]ServiceOutputs),
		Routes:         make(map[string]RouteOutputs),
		Functions:      make(map[string]FunctionOutputs),
		Variables:      make(map[string]interface{}),
		Dependencies:   make(map[string]DependencyOutputs),
		Dependents:     make(map[string]DependentOutputs),
	}
}

// Evaluator evaluates parsed expressions.
type Evaluator struct {
	functions map[string]PipeFuncImpl
}

// PipeFuncImpl is the implementation of a pipe function.
type PipeFuncImpl func(value interface{}, args []string) (interface{}, error)

// NewEvaluator creates a new expression evaluator.
func NewEvaluator() *Evaluator {
	return &Evaluator{
		functions: map[string]PipeFuncImpl{
			"join":   joinFunc,
			"first":  firstFunc,
			"last":   lastFunc,
			"length": lengthFunc,
			"default": defaultFunc,
			"upper":  upperFunc,
			"lower":  lowerFunc,
			"trim":   trimFunc,
		},
	}
}

// Evaluate evaluates an expression in the given context.
func (e *Evaluator) Evaluate(expr *Expression, ctx *EvalContext) (interface{}, error) {
	if len(expr.Segments) == 0 {
		return "", nil
	}

	// If only a single reference segment, return the actual value (not stringified)
	if len(expr.Segments) == 1 {
		if ref, ok := expr.Segments[0].(ReferenceSegment); ok {
			return e.evaluateReference(ref, ctx)
		}
		if lit, ok := expr.Segments[0].(LiteralSegment); ok {
			return lit.Value, nil
		}
	}

	// Multiple segments - concatenate as string
	var result strings.Builder
	for _, seg := range expr.Segments {
		switch s := seg.(type) {
		case LiteralSegment:
			result.WriteString(s.Value)
		case ReferenceSegment:
			val, err := e.evaluateReference(s, ctx)
			if err != nil {
				return nil, err
			}
			result.WriteString(fmt.Sprintf("%v", val))
		}
	}

	return result.String(), nil
}

// EvaluateString evaluates an expression and returns a string result.
func (e *Evaluator) EvaluateString(expr *Expression, ctx *EvalContext) (string, error) {
	val, err := e.Evaluate(expr, ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", val), nil
}

func (e *Evaluator) evaluateReference(ref ReferenceSegment, ctx *EvalContext) (interface{}, error) {
	if len(ref.Path) == 0 {
		return nil, fmt.Errorf("empty reference path")
	}

	var value interface{}
	var err error

	switch ref.Path[0] {
	case "builds":
		value, err = e.resolveBuild(ref.Path[1:], ctx.Builds)
	case "databases":
		value, err = e.resolveDatabase(ref.Path[1:], ctx.Databases)
	case "buckets":
		value, err = e.resolveBucket(ref.Path[1:], ctx.Buckets)
	case "encryptionKeys":
		value, err = e.resolveEncryptionKey(ref.Path[1:], ctx.EncryptionKeys)
	case "smtp":
		value, err = e.resolveSMTP(ref.Path[1:], ctx.SMTP)
	case "services":
		value, err = e.resolveService(ref.Path[1:], ctx.Services)
	case "routes":
		value, err = e.resolveRoute(ref.Path[1:], ctx.Routes)
	case "functions":
		value, err = e.resolveFunction(ref.Path[1:], ctx.Functions)
	case "variables":
		value, err = e.resolveVariable(ref.Path[1:], ctx.Variables)
	case "dependencies":
		value, err = e.resolveDependency(ref.Path[1:], ctx.Dependencies)
	case "dependents":
		value, err = e.resolveDependents(ref.Path[1:], ctx.Dependents)
	case "ports":
		value, err = e.resolvePort(ref.Path[1:], ctx.Ports)
	case "observability":
		value, err = e.resolveObservability(ref.Path[1:], ctx.Observability)
	default:
		return nil, fmt.Errorf("unknown reference type: %s", ref.Path[0])
	}

	if err != nil {
		return nil, err
	}

	// Apply pipe functions
	for _, pf := range ref.Pipe {
		fn, ok := e.functions[pf.Name]
		if !ok {
			return nil, fmt.Errorf("unknown pipe function: %s", pf.Name)
		}
		value, err = fn(value, pf.Args)
		if err != nil {
			return nil, fmt.Errorf("pipe function %s failed: %w", pf.Name, err)
		}
	}

	return value, nil
}

func (e *Evaluator) resolveBuild(path []string, builds map[string]BuildOutputs) (interface{}, error) {
	if len(path) < 2 {
		return nil, fmt.Errorf("invalid build reference: need name and property")
	}

	name := path[0]
	prop := path[1]

	build, ok := builds[name]
	if !ok {
		return nil, fmt.Errorf("build %q not found", name)
	}

	switch prop {
	case "image":
		return build.Image, nil
	default:
		return nil, fmt.Errorf("unknown build property: %s", prop)
	}
}

func (e *Evaluator) resolveDatabase(path []string, databases map[string]DatabaseOutputs) (interface{}, error) {
	if len(path) < 2 {
		return nil, fmt.Errorf("invalid database reference: need name and property")
	}

	name := path[0]
	prop := path[1]

	db, ok := databases[name]
	if !ok {
		return nil, fmt.Errorf("database %q not found", name)
	}

	// Handle read/write sub-objects: databases.<name>.read.<prop> / databases.<name>.write.<prop>
	if prop == "read" || prop == "write" {
		if len(path) < 3 {
			return nil, fmt.Errorf("invalid database %s reference: need property (e.g., %s.url)", prop, prop)
		}
		subProp := path[2]
		return e.resolveDatabaseEndpoint(db, prop, subProp)
	}

	switch prop {
	case "host":
		return db.Host, nil
	case "port":
		return db.Port, nil
	case "database":
		return db.Database, nil
	case "username":
		return db.Username, nil
	case "password":
		return db.Password, nil
	case "url":
		return db.URL, nil
	default:
		return nil, fmt.Errorf("unknown database property: %s", prop)
	}
}

// resolveDatabaseEndpoint resolves a read or write endpoint property, falling back
// to the top-level database values when the endpoint is nil.
func (e *Evaluator) resolveDatabaseEndpoint(db DatabaseOutputs, direction, prop string) (interface{}, error) {
	var endpoint *DatabaseEndpoint
	if direction == "read" {
		endpoint = db.Read
	} else {
		endpoint = db.Write
	}

	// Fall back to top-level values when the endpoint is not explicitly set
	if endpoint == nil {
		switch prop {
		case "host":
			return db.Host, nil
		case "port":
			return db.Port, nil
		case "username":
			return db.Username, nil
		case "password":
			return db.Password, nil
		case "url":
			return db.URL, nil
		default:
			return nil, fmt.Errorf("unknown database %s property: %s", direction, prop)
		}
	}

	switch prop {
	case "host":
		return endpoint.Host, nil
	case "port":
		return endpoint.Port, nil
	case "username":
		return endpoint.Username, nil
	case "password":
		return endpoint.Password, nil
	case "url":
		return endpoint.URL, nil
	default:
		return nil, fmt.Errorf("unknown database %s property: %s", direction, prop)
	}
}

func (e *Evaluator) resolveBucket(path []string, buckets map[string]BucketOutputs) (interface{}, error) {
	if len(path) < 2 {
		return nil, fmt.Errorf("invalid bucket reference: need name and property")
	}

	name := path[0]
	prop := path[1]

	bucket, ok := buckets[name]
	if !ok {
		return nil, fmt.Errorf("bucket %q not found", name)
	}

	switch prop {
	case "endpoint":
		return bucket.Endpoint, nil
	case "bucket":
		return bucket.Bucket, nil
	case "region":
		return bucket.Region, nil
	case "accessKeyId":
		return bucket.AccessKeyID, nil
	case "secretAccessKey":
		return bucket.SecretAccessKey, nil
	default:
		return nil, fmt.Errorf("unknown bucket property: %s", prop)
	}
}

func (e *Evaluator) resolveEncryptionKey(path []string, encryptionKeys map[string]EncryptionKeyOutputs) (interface{}, error) {
	if len(path) < 2 {
		return nil, fmt.Errorf("invalid encryption key reference: need name and property")
	}

	name := path[0]
	prop := path[1]

	ek, ok := encryptionKeys[name]
	if !ok {
		return nil, fmt.Errorf("encryption key %q not found", name)
	}

	switch prop {
	// Asymmetric key outputs (RSA/ECDSA)
	case "privateKey":
		return ek.PrivateKey, nil
	case "publicKey":
		return ek.PublicKey, nil
	case "privateKeyBase64":
		return ek.PrivateKeyBase64, nil
	case "publicKeyBase64":
		return ek.PublicKeyBase64, nil
	// Symmetric/Salt outputs
	case "key":
		return ek.Key, nil
	case "keyBase64":
		return ek.KeyBase64, nil
	default:
		return nil, fmt.Errorf("unknown encryption key property: %s", prop)
	}
}

func (e *Evaluator) resolvePort(path []string, ports map[string]PortOutputs) (interface{}, error) {
	if len(path) < 2 {
		return nil, fmt.Errorf("invalid port reference: need name and property")
	}

	name := path[0]
	prop := path[1]

	p, ok := ports[name]
	if !ok {
		return nil, fmt.Errorf("port %q not found", name)
	}

	switch prop {
	case "port":
		return p.Port, nil
	default:
		return nil, fmt.Errorf("unknown port property: %s", prop)
	}
}

func (e *Evaluator) resolveSMTP(path []string, smtp map[string]SMTPOutputs) (interface{}, error) {
	if len(path) < 2 {
		return nil, fmt.Errorf("invalid smtp reference: need name and property")
	}

	name := path[0]
	prop := path[1]

	s, ok := smtp[name]
	if !ok {
		return nil, fmt.Errorf("smtp %q not found", name)
	}

	switch prop {
	case "host":
		return s.Host, nil
	case "port":
		return s.Port, nil
	case "username":
		return s.Username, nil
	case "password":
		return s.Password, nil
	default:
		return nil, fmt.Errorf("unknown smtp property: %s", prop)
	}
}

func (e *Evaluator) resolveService(path []string, services map[string]ServiceOutputs) (interface{}, error) {
	if len(path) < 2 {
		return nil, fmt.Errorf("invalid service reference: need name and property")
	}

	name := path[0]
	prop := path[1]

	svc, ok := services[name]
	if !ok {
		return nil, fmt.Errorf("service %q not found", name)
	}

	switch prop {
	case "url":
		return svc.URL, nil
	case "host":
		return svc.Host, nil
	case "port":
		return svc.Port, nil
	case "protocol":
		return svc.Protocol, nil
	default:
		return nil, fmt.Errorf("unknown service property: %s", prop)
	}
}

func (e *Evaluator) resolveRoute(path []string, routes map[string]RouteOutputs) (interface{}, error) {
	if len(path) < 2 {
		return nil, fmt.Errorf("invalid route reference: need name and property")
	}

	name := path[0]
	prop := path[1]

	route, ok := routes[name]
	if !ok {
		return nil, fmt.Errorf("route %q not found", name)
	}

	switch prop {
	case "url":
		return route.URL, nil
	case "hosts":
		return route.Hosts, nil
	default:
		return nil, fmt.Errorf("unknown route property: %s", prop)
	}
}

func (e *Evaluator) resolveFunction(path []string, functions map[string]FunctionOutputs) (interface{}, error) {
	if len(path) < 2 {
		return nil, fmt.Errorf("invalid function reference: need name and property")
	}

	name := path[0]
	prop := path[1]

	fn, ok := functions[name]
	if !ok {
		return nil, fmt.Errorf("function %q not found", name)
	}

	switch prop {
	case "url":
		return fn.URL, nil
	case "id":
		return fn.ID, nil
	default:
		return nil, fmt.Errorf("unknown function property: %s", prop)
	}
}

func (e *Evaluator) resolveVariable(path []string, variables map[string]interface{}) (interface{}, error) {
	if len(path) < 1 {
		return nil, fmt.Errorf("invalid variable reference: need name")
	}

	name := path[0]
	val, ok := variables[name]
	if !ok {
		return nil, fmt.Errorf("variable %q not found", name)
	}

	return val, nil
}

func (e *Evaluator) resolveDependency(path []string, dependencies map[string]DependencyOutputs) (interface{}, error) {
	if len(path) < 2 {
		return nil, fmt.Errorf("invalid dependency reference: need name and type")
	}

	name := path[0]
	dep, ok := dependencies[name]
	if !ok {
		return nil, fmt.Errorf("dependency %q not found", name)
	}

	resourceType := path[1]

	switch resourceType {
	case "services":
		if len(path) < 3 {
			return nil, fmt.Errorf("invalid dependency service reference: need service name")
		}
		resourceName := path[2]
		svc, ok := dep.Services[resourceName]
		if !ok {
			return nil, fmt.Errorf("service %q not found in dependency %q", resourceName, name)
		}
		if len(path) > 3 {
			return e.resolveServiceProperty(svc, path[3])
		}
		return svc, nil

	case "routes":
		if len(path) < 3 {
			return nil, fmt.Errorf("invalid dependency route reference: need route name")
		}
		resourceName := path[2]
		route, ok := dep.Routes[resourceName]
		if !ok {
			return nil, fmt.Errorf("route %q not found in dependency %q", resourceName, name)
		}
		if len(path) > 3 {
			return e.resolveRouteProperty(route, path[3])
		}
		return route, nil

	case "outputs":
		if len(path) < 3 {
			return nil, fmt.Errorf("invalid dependency output reference: need output name")
		}
		outputName := path[2]
		if dep.Outputs == nil {
			return nil, fmt.Errorf("dependency %q has no outputs", name)
		}
		val, ok := dep.Outputs[outputName]
		if !ok {
			return nil, fmt.Errorf("output %q not found in dependency %q", outputName, name)
		}
		return val, nil

	default:
		return nil, fmt.Errorf("unknown dependency resource type: %s", resourceType)
	}
}

func (e *Evaluator) resolveDependents(path []string, dependents map[string]DependentOutputs) (interface{}, error) {
	// Handle wildcard: dependents.*.routes.*.url
	if len(path) > 0 && path[0] == "*" {
		return e.resolveWildcardDependents(path[1:], dependents)
	}

	if len(path) < 3 {
		return nil, fmt.Errorf("invalid dependents reference")
	}

	name := path[0]
	dep, ok := dependents[name]
	if !ok {
		return nil, fmt.Errorf("dependent %q not found", name)
	}

	resourceType := path[1]
	resourceName := path[2]

	switch resourceType {
	case "services":
		svc, ok := dep.Services[resourceName]
		if !ok {
			return nil, fmt.Errorf("service %q not found in dependent %q", resourceName, name)
		}
		if len(path) > 3 {
			return e.resolveServiceProperty(svc, path[3])
		}
		return svc, nil

	case "routes":
		route, ok := dep.Routes[resourceName]
		if !ok {
			return nil, fmt.Errorf("route %q not found in dependent %q", resourceName, name)
		}
		if len(path) > 3 {
			return e.resolveRouteProperty(route, path[3])
		}
		return route, nil

	default:
		return nil, fmt.Errorf("unknown dependent resource type: %s", resourceType)
	}
}

func (e *Evaluator) resolveWildcardDependents(path []string, dependents map[string]DependentOutputs) (interface{}, error) {
	// Collect values from all dependents
	var values []interface{}

	if len(path) < 2 {
		return nil, fmt.Errorf("invalid wildcard reference")
	}

	resourceType := path[0]

	for _, dep := range dependents {
		switch resourceType {
		case "routes":
			if path[1] == "*" {
				// All routes
				for _, route := range dep.Routes {
					if len(path) > 2 {
						val, _ := e.resolveRouteProperty(route, path[2])
						if val != nil {
							values = append(values, val)
						}
					}
				}
			} else {
				route, ok := dep.Routes[path[1]]
				if ok {
					if len(path) > 2 {
						val, _ := e.resolveRouteProperty(route, path[2])
						if val != nil {
							values = append(values, val)
						}
					}
				}
			}

		case "services":
			if path[1] == "*" {
				for _, svc := range dep.Services {
					if len(path) > 2 {
						val, _ := e.resolveServiceProperty(svc, path[2])
						if val != nil {
							values = append(values, val)
						}
					}
				}
			} else {
				svc, ok := dep.Services[path[1]]
				if ok {
					if len(path) > 2 {
						val, _ := e.resolveServiceProperty(svc, path[2])
						if val != nil {
							values = append(values, val)
						}
					}
				}
			}
		}
	}

	return values, nil
}

func (e *Evaluator) resolveObservability(path []string, obs *ObservabilityOutputs) (interface{}, error) {
	if obs == nil {
		return nil, fmt.Errorf("observability not configured")
	}

	if len(path) < 1 {
		return nil, fmt.Errorf("invalid observability reference: need property")
	}

	prop := path[0]
	switch prop {
	case "endpoint":
		return obs.Endpoint, nil
	case "protocol":
		return obs.Protocol, nil
	case "attributes":
		return obs.Attributes, nil
	default:
		return nil, fmt.Errorf("unknown observability property: %s", prop)
	}
}

func (e *Evaluator) resolveServiceProperty(svc ServiceOutputs, prop string) (interface{}, error) {
	switch prop {
	case "url":
		return svc.URL, nil
	case "host":
		return svc.Host, nil
	case "port":
		return svc.Port, nil
	default:
		return nil, fmt.Errorf("unknown service property: %s", prop)
	}
}

func (e *Evaluator) resolveRouteProperty(route RouteOutputs, prop string) (interface{}, error) {
	switch prop {
	case "url":
		return route.URL, nil
	case "hosts":
		return route.Hosts, nil
	default:
		return nil, fmt.Errorf("unknown route property: %s", prop)
	}
}
