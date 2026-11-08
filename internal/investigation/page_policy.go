package investigation

// connectionsIncludeTooling is the Connections page's effective visibility
// policy, not an API default. A name search includes matching tooling even
// with tooling=0. Both import and link-equivalence checks use this policy;
// explicit request fields are applied afterward. Cross-language contract
// fixtures are exercised against the actual UI's outgoing requests.
func connectionsIncludeTooling(name string, tooling bool) bool {
	return tooling || name != ""
}
