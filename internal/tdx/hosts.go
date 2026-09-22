// TDX host pool management: probe reachability up front and rotate workers
// over healthy hosts only.
package tdx

import (
	"sort"
	"sync"
	"time"

	"github.com/bensema/gotdx"
)

var (
	hostMu   sync.RWMutex
	hostPool []string
)

// SetHostPool overrides the address pool NewRotated rotates over.
func SetHostPool(addresses []string) {
	if len(addresses) == 0 {
		return
	}
	hostMu.Lock()
	hostPool = addresses
	hostMu.Unlock()
}

func currentHosts() []string {
	hostMu.RLock()
	defer hostMu.RUnlock()
	if len(hostPool) > 0 {
		return hostPool
	}
	return gotdx.MainHostAddresses()
}

// AllHosts returns the unfiltered main pool size for logging.
func AllHosts() []string { return gotdx.MainHostAddresses() }

// HealthyHosts probes the main TDX pool and returns reachable addresses,
// lowest latency first. Falls back to the unfiltered pool when nothing
// answers (e.g. a probing blacklist).
func HealthyHosts() []string {
	hosts := gotdx.MainHostAddresses()
	results := gotdx.ProbeAddresses(hosts, 3*time.Second)
	type pick struct {
		addr    string
		latency time.Duration
	}
	var picks []pick
	for _, r := range results {
		if r.Reachable {
			picks = append(picks, pick{r.Address, r.Latency})
		}
	}
	sort.Slice(picks, func(i, j int) bool { return picks[i].latency < picks[j].latency })
	out := make([]string, 0, len(picks))
	for _, p := range picks {
		out = append(out, p.addr)
	}
	if len(out) == 0 {
		return hosts
	}
	return out
}
