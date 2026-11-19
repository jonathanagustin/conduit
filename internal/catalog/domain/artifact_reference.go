package domain

import "strings"

// ParseImmutableArtifactReference retains the repository/tag/digest import
// grammar used by Product manifests. It does not replace ValidateArtifact or
// establish complete OCI reference validation.
func ParseImmutableArtifactReference(reference string) (Artifact, bool) {
	reference = strings.TrimPrefix(strings.TrimSpace(reference), "oci://")
	parts := strings.Split(reference, "@")
	if len(parts) != 2 || !artifactDigestPattern.MatchString(parts[1]) {
		return Artifact{}, false
	}
	colon, slash := strings.LastIndex(parts[0], ":"), strings.LastIndex(parts[0], "/")
	if colon <= slash || colon == len(parts[0])-1 {
		return Artifact{}, false
	}
	artifact := Artifact{Repository: parts[0][:colon], Tag: parts[0][colon+1:], Digest: parts[1]}
	if artifact.Repository == "" || artifact.Tag == "latest" {
		return Artifact{}, false
	}
	return artifact, true
}
