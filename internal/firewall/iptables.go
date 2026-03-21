package firewall

import (
	"fmt"
	"os/exec"
	"strconv"
)

// Rules holds the configuration for iptables rules inside the Discord namespace
type Rules struct {
	proxyPort  int
	nfqueueNum uint16
	nsName     string
}

// New creates a new Rules instance
func New(proxyPort int, nfqueueNum uint16, nsName string) *Rules {
	return &Rules{
		proxyPort:  proxyPort,
		nfqueueNum: nfqueueNum,
		nsName:     nsName,
	}
}

// Apply sets up iptables rules inside the Discord namespace.
// In direct mode (no proxy), only UDP is intercepted for DPI bypass.
// In proxy mode, TCP is also redirected to the local transparent proxy.
func (r *Rules) Apply(directMode bool) error {
	queueStr := strconv.Itoa(int(r.nfqueueNum))

	// Flush existing rules first
	if err := r.runInNs("iptables", "-F"); err != nil {
		return fmt.Errorf("failed to flush filter rules: %w", err)
	}
	if err := r.runInNs("iptables", "-t", "nat", "-F"); err != nil {
		return fmt.Errorf("failed to flush nat rules: %w", err)
	}

	// TCP redirect — only when a real proxy port is available
	if !directMode && r.proxyPort > 0 {
		if err := r.runInNs(
			"iptables", "-t", "nat", "-A", "OUTPUT",
			"-p", "tcp",
			"!", "-d", "192.168.250.0/24",
			"-j", "REDIRECT", "--to-port", strconv.Itoa(r.proxyPort),
		); err != nil {
			return fmt.Errorf("failed to add TCP redirect rule: %w", err)
		}
	}

	// UDP — always sent to NFQUEUE for DPI bypass
	if err := r.runInNs(
		"iptables", "-A", "OUTPUT",
		"-p", "udp",
		"!", "-d", "192.168.250.0/24",
		"-j", "NFQUEUE", "--queue-num", queueStr,
	); err != nil {
		return fmt.Errorf("failed to add UDP NFQUEUE rule: %w", err)
	}

	return nil
}

// runInNs executes an iptables command inside the Discord network namespace
func (r *Rules) runInNs(args ...string) error {
	cmd := exec.Command("ip", append([]string{"netns", "exec", r.nsName}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error: %s", string(output))
	}
	return nil
}

// Cleanup removes all iptables rules from the Discord namespace
func (r *Rules) Cleanup() {
	r.runInNs("iptables", "-F")
	r.runInNs("iptables", "-t", "nat", "-F")
}
