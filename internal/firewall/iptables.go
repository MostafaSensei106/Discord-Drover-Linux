package firewall

import (
	"fmt"
	"os/exec"
	"strconv"
)

type Rules struct {
	proxyPort  int
	nfqueueNum uint16
	nsName     string
}

func New(proxyPort int, nfqueueNum uint16, nsName string) *Rules {
	return &Rules{
		proxyPort:  proxyPort,
		nfqueueNum: nfqueueNum,
		nsName:     nsName,
	}
}

func (r *Rules) Apply() error {
	queueStr := strconv.Itoa(int(r.nfqueueNum))

	comands := [][]string{
		{"iptables", "-F"},
		{"iptables", "-t", "nat", "-F"},

		{"iptables", "-t", "nat", "-A", "OUTPUT",
			"-p", "tcp",
			"!", "-d", "192.168.250.0/24",
			"-j", "REDIRECT", "--to-port", strconv.Itoa(r.proxyPort)},

		{"iptables", "-A", "OUTPUT",
			"-p", "udp",
			"!", "-d", "192.168.250.0/24",
			"-j", "NFQUEUE", "--queue-num", queueStr},
	}

	for _, cmd := range comands {
		if err := r.runInNs(cmd...); err != nil {
			return fmt.Errorf("failed to apply %v: %w", cmd, err)

		}

	}
	return nil
}

func (r *Rules) runInNs(args ...string) error {
	cmd := exec.Command("ip", append([]string{"netns", "exec", r.nsName}, args...)...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error: %s", string(output))
	}
	return nil
}

func (r *Rules) Cleanup() {
	r.runInNs("iptables", "-F")
	r.runInNs("iptables", "-t", "nat", "-F")
}
