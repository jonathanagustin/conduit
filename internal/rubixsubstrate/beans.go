package rubixsubstrate

// The recovered Rubix networking and credential wire beans.
//
// Provenance: field-for-field from the Conjure objects, unions, aliases and
// enums in com.palantir.rubixlib:network-api:2.512.0 and
// :user-credential-manager:2.512.0, read out of the recovered jar bytecode
// (javap). These are the exact request/response shapes the fourteen endpoints in
// routes.go carry -- not authored here. JSON tags are the Conjure field names,
// so a value marshals to the wire format the tenant serves. Conjure sets become
// slices, optionals become pointers with omitempty, and unions become a
// discriminated struct, matching Conjure's JSON representation.
//
// beans_test.go binds every type below to deploy/rubix-substrate/wire-beans.json
// -- the recovered field inventory -- so the Go surface cannot drift from the
// ground truth without failing the Test gate.

// --- aliases (Conjure alias types) ---

type Cidr string
type CountryIsoCode string
type Domain string
type Protocol string
type Port int

type ClusterRoleName string
type ConcreteNamespace string
type Credential string
type EnvironmentID string
type NamespaceType string
type Password string
type Scope string
type ServiceName string
type Username string

// EgressConfigurations is a Conjure set alias (set<EgressConfiguration>).
type EgressConfigurations []EgressConfiguration

// GetEgressCidrsResponse is a Conjure set alias (set<Cidr>).
type GetEgressCidrsResponse []Cidr

// IngressConfigurations is a Conjure map alias (map<Domain, IngressConfiguration>).
type IngressConfigurations map[Domain]IngressConfiguration

// --- enums ---

// ClusterMarking is the recovered Conjure enum ClusterMarkings, with the UNKNOWN
// escape hatch Conjure emits for forward compatibility.
type ClusterMarking string

const (
	ClusterMarkingInternalRoutingAvailable       ClusterMarking = "INTERNAL_ROUTING_AVAILABLE"
	ClusterMarkingEnvironmentProxyClientRequired ClusterMarking = "ENVIRONMENT_PROXY_CLIENT_REQUIRED"
	ClusterMarkingUnknown                        ClusterMarking = "UNKNOWN"
)

// --- unions (Conjure discriminated unions) ---

// EgressConfiguration is the recovered union: exactly one variant is set, and
// Type names it, matching Conjure's {"type":"...","<variant>":{...}} encoding.
type EgressConfiguration struct {
	Type                      string                     `json:"type"`
	TCPEgressConfiguration    *TCPEgressConfiguration    `json:"tcpEgressConfiguration,omitempty"`
	DomainEgressConfiguration *DomainEgressConfiguration `json:"domainEgressConfiguration,omitempty"`
}

// PortSpecifier is the recovered union of a single Port or a PortRange.
type PortSpecifier struct {
	Type      string     `json:"type"`
	Port      *Port      `json:"port,omitempty"`
	PortRange *PortRange `json:"portRange,omitempty"`
}

// --- objects (Conjure objects) ---

type ApplyNetworkEgressConfigurationRequest struct {
	EgressConfigurations EgressConfigurations `json:"egressConfigurations"`
}

// ApplyNetworkEgressConfigurationResponse is a recovered empty Conjure object.
type ApplyNetworkEgressConfigurationResponse struct{}

// ApplyNetworkIngressRollbackResponse is a recovered empty Conjure object.
type ApplyNetworkIngressRollbackResponse struct{}

// SetDomainIngressResponse is a recovered empty Conjure object.
type SetDomainIngressResponse struct{}

type DomainEgressConfiguration struct {
	Domain        Domain        `json:"domain"`
	Protocol      Protocol      `json:"protocol"`
	PortSpecifier PortSpecifier `json:"portSpecifier"`
}

type TCPEgressConfiguration struct {
	Name                 string        `json:"name"`
	Cidr                 Cidr          `json:"cidr"`
	PortSpecifier        PortSpecifier `json:"portSpecifier"`
	DisableTLSInspection bool          `json:"disableTlsInspection"`
}

type PortRange struct {
	PortRangeStart Port `json:"portRangeStart"`
	PortRangeEnd   Port `json:"portRangeEnd"`
}

type IngressConfiguration struct {
	CountryCodes []CountryIsoCode `json:"countryCodes"`
	Cidrs        []Cidr           `json:"cidrs"`
}

type GetFallbackConfigResponse struct {
	GlobalIngressConfiguration IngressConfiguration `json:"globalIngressConfiguration"`
}

type SetDomainIngressRequest struct {
	Domain               Domain               `json:"domain"`
	IngressConfiguration IngressConfiguration `json:"ingressConfiguration"`
}

type BindCredentialRequest struct {
	Password      Password          `json:"password"`
	EnvironmentID EnvironmentID     `json:"environmentId"`
	ClusterRoles  []ClusterRoleName `json:"clusterRoles"`
	ServiceName   *ServiceName      `json:"serviceName,omitempty"`
}

type Cluster struct {
	ApolloEnvironmentID             string           `json:"apolloEnvironmentId"`
	ComputeFrontdoorURIs            []string         `json:"computeFrontdoorUris"`
	ClusterMarkings                 []ClusterMarking `json:"clusterMarkings"`
	APIServerURIs                   []string         `json:"apiServerUris"`
	UserCredentialManagerServerURIs []string         `json:"userCredentialManagerServerUris"`
}

type ClusterConfigurations struct {
	Clusters []Cluster `json:"clusters"`
}

type RetrieveClusterConfigurationsRequest struct {
	Password      Password      `json:"password"`
	EnvironmentID EnvironmentID `json:"environmentId"`
	ServiceName   *ServiceName  `json:"serviceName,omitempty"`
}

type RetrieveCredentialForInfraUserPayload struct {
	Credential Credential `json:"credential"`
}

type RetrieveCredentialForInfraUserRequest struct {
	Password Password `json:"password"`
}

type RetrieveCredentialPayload struct {
	Credential               Credential                            `json:"credential"`
	MappedNamespaces         map[NamespaceType]ConcreteNamespace   `json:"mappedNamespaces"`
	MultipleMappedNamespaces map[NamespaceType][]ConcreteNamespace `json:"multipleMappedNamespaces"`
}

type RetrieveCredentialRequest struct {
	Password      Password      `json:"password"`
	EnvironmentID EnvironmentID `json:"environmentId"`
	ServiceName   *ServiceName  `json:"serviceName,omitempty"`
}

type RetrieveServiceAccountCredentialFromTokenPayload struct {
	Credential Credential `json:"credential"`
}

type ValidateConfigRequest struct {
	Password      Password      `json:"password"`
	EnvironmentID EnvironmentID `json:"environmentId"`
	ServiceName   *ServiceName  `json:"serviceName,omitempty"`
}

type ValidationPayload struct {
	SyntaxValid    bool `json:"syntaxValid"`
	SemanticsValid bool `json:"semanticsValid"`
}
