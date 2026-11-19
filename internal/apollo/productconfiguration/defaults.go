package productconfiguration

// Existing defaults apply only when the corresponding declaration is absent.
const (
	DefaultReplication = 2
	DefaultCPU         = "0.25"
	DefaultMemory      = "1G"
)

// EffectiveResources fills in what a definition left unsaid. A lone limit
// becomes its own request, so a Product that states only a ceiling is scheduled
// against that ceiling rather than against the platform default.
func EffectiveResources(resources *ResourceRequirements) ResourceRequirements {
	requests := ResourceQuantities{}
	limits := ResourceQuantities{}
	if resources != nil {
		if resources.Requests != nil {
			requests = *resources.Requests
		}
		if resources.Limits != nil {
			limits = *resources.Limits
		}
	}
	if requests.CPU == "" {
		requests.CPU = limits.CPU
	}
	if requests.Memory == "" {
		requests.Memory = limits.Memory
	}
	if requests.CPU == "" {
		requests.CPU = DefaultCPU
	}
	if requests.Memory == "" {
		requests.Memory = DefaultMemory
	}
	return ResourceRequirements{Requests: &requests, Limits: &limits}
}

// EffectiveReplication is the replica count an installation runs at when
// neither the definition nor its overrides state one.
func EffectiveReplication(replication *Replication) int {
	if replication == nil || replication.Desired == nil {
		return DefaultReplication
	}
	return *replication.Desired
}
