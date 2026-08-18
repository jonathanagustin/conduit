package buildinfo

// ScratchGofmtProbe is deliberately misformatted so the go lane fails its first
// gate. Scratch branch for the #213 failure-path evidence; never merged.
func ScratchGofmtProbe( ) int {
	return      1
}
