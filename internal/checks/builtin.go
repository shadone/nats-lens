package checks

// The definitions of the two built-in checks evaluated in Go, by
// server_slow.go and server_leaf.go. They live apart from those files
// because the package also builds for WebAssembly, where the evaluation and
// the store it reads are left out (see engine.go).

// SlowRouteCode is the built-in check sharing Topology's classifier.
const SlowRouteCode = "server-025"

func slowRouteCheck() *Check {
	return &Check{
		Code: SlowRouteCode, Name: "Slow routes", Entity: "server", Category: "health", Severity: "warning", Kind: "operational",
		Description: "One or more of the server's route pairs are slow by Topology's rule in two eligible collections. Both ends receive a finding; a slow link does not establish which end is responsible. The previous route-bearing collection must be within three collect intervals. An unknown interval or unreported RTT cannot establish slowness.",
		Remediation: "Read the slow pairs on the Topology page. Compare the named peers' network latency, packet loss, CPU and scheduling pressure with the cluster's other servers. Check zone placement and network paths before changing configuration. nats-lens only observes the routes.",
		ValueName:   "slow route pairs", ValueBasis: "pairs touching this server classified slow in both eligible collections by Topology's shared rule",
		builtin: true,
	}
}

const LeafHealthCode = "server-026"

func leafHealthCheck() *Check {
	return &Check{Code: LeafHealthCode, Name: "Leaf node disconnects or repeated reconnects", Entity: "system", Category: "health", Severity: "warning", Kind: "operational", builtin: true,
		Description: "Observed leaf sites absent for two consecutive fully observed collections, or with at least three observed connection drops in one hour. Hub handoffs are excluded. A system finding names sites and accounts without blaming a hub. Missing LEAFZ, restarts and gaps break evidence; unobserved expected sites cannot fire.",
		Remediation: "Read the named sites on Topology. Compare their network paths and remote process logs with the hub logs. Confirm whether the absence or reconnects are expected before changing configuration. NATS provides no leaf disconnect event; these are collection observations.",
		ValueName:   "affected leaf sites", ValueBasis: "site name (or IP fallback) and account pairs meeting either observed leaf health condition"}
}
