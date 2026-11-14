package domain

// HasPublication reports a catalogue identity even when the publication carries
// no executable Release. Empty version selects the Product rather than one of
// its Releases. Metadata-only publications retain the same retirement identity.
func HasPublication(publications []Publication, product, version string) bool {
	for _, publication := range publications {
		if publication.Product == product && (version == "" || publication.Version == version) {
			return true
		}
	}
	return false
}
