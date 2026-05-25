package energy

// Index returns a normalized resource-consumption score.
//
// The score is not a physical energy measurement in Wh/J. It is a single
// relative unit for comparing benchmark runs in the same test stand:
//
//	EnergyIndex = N * (1 + CPU/100) + NetworkMB + (N * MemoryMB / 1024)
//
// where CPU is average CPU utilization in percent, MemoryMB is average memory
// per node, and NetworkMB is estimated network traffic.
func Index(nodes int, cpuPercent, memoryMB, networkMB float64) float64 {
	if nodes < 0 {
		nodes = 0
	}
	if cpuPercent < 0 {
		cpuPercent = 0
	}
	if memoryMB < 0 {
		memoryMB = 0
	}
	if networkMB < 0 {
		networkMB = 0
	}

	n := float64(nodes)
	computeCost := n * (1 + cpuPercent/100.0)
	networkCost := networkMB
	stateCost := n * memoryMB / 1024.0
	return computeCost + networkCost + stateCost
}
