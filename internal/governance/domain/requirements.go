package domain

// AllRequirementsSatisfied reports whether every requirement raised against a
// change has been met.
//
// A requirement is met when something is recorded in SatisfiedBy, not when it
// looks satisfiable. The distinction is the point: a reviewer who *could* meet
// a requirement has not met it until the review exists, and an empty
// requirement list means nothing was demanded rather than that nothing was
// answered.
func AllRequirementsSatisfied(requirements []ReviewerRequirement) bool {
	for _, requirement := range requirements {
		if len(requirement.SatisfiedBy) == 0 {
			return false
		}
	}
	return true
}
