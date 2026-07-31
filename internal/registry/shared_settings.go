package registry

// ResolveSharedSettings resolves fail_on and consensus together.
//
// STUB (RED): delegates to the two independent resolvers, which is exactly the
// duplicate-I/O behavior this function exists to eliminate. Replaced by a
// single-load implementation in the following commit.
func ResolveSharedSettings(root, explicitFailOn, explicitConsensus string) (failOn, consensus string, err error) {
	failOn, err = ResolveGateThreshold(root, explicitFailOn)
	if err != nil {
		return "", "", err
	}
	consensus, err = ResolveConsensus(root, explicitConsensus)
	if err != nil {
		return "", "", err
	}
	return failOn, consensus, nil
}
