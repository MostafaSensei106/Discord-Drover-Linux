package netns

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/MostafaSensei106/Discord-Drover-Linux/internal/constants"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// NetworkNamespace holds handles for the Discord-specific network namespace
type NetworkNamespace struct {
	nsHandle   netns.NsHandle
	hostNs     netns.NsHandle
	vethHost   string
	vethPeerNs string
}

// Setup creates a new named network namespace and wires it to the host via a veth pair
func Setup() (*NetworkNamespace, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hostNs, err := netns.Get()
	if err != nil {
		return nil, fmt.Errorf("failed to get host network namespace: %w", err)
	}

	// Remove any leftover namespace from a previous run
	_ = netns.DeleteNamed(constants.ToolNamespace)

	nsHandle, err := netns.NewNamed(constants.ToolNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create named network namespace %q: %w", constants.ToolNamespace, err)
	}

	ns := &NetworkNamespace{
		nsHandle:   nsHandle,
		hostNs:     hostNs,
		vethHost:   "veth-host",
		vethPeerNs: "veth-peer",
	}

	// Return to host namespace before configuring interfaces
	if err := netns.Set(hostNs); err != nil {
		return nil, fmt.Errorf("failed to switch back to host namespace: %w", err)
	}

	if err := ns.setupVethPair(); err != nil {
		return nil, fmt.Errorf("failed to setup veth pair: %w", err)
	}

	if err := ns.setupRouting(); err != nil {
		return nil, fmt.Errorf("failed to setup routing: %w", err)
	}

	return ns, nil
}

// setupVethPair creates the virtual ethernet cable between host and namespace
func (n *NetworkNamespace) setupVethPair() error {
	// Remove any leftover veth from a previous run
	_ = netlink.LinkDel(&netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: n.vethHost}})

	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{Name: n.vethHost},
		PeerName:  n.vethPeerNs,
	}
	if err := netlink.LinkAdd(veth); err != nil {
		return fmt.Errorf("failed to add veth pair: %w", err)
	}

	// Configure the host-side interface
	hostLink, err := netlink.LinkByName(n.vethHost)
	if err != nil {
		return fmt.Errorf("failed to get host link: %w", err)
	}

	hostIP, hostNet, _ := net.ParseCIDR("192.168.250.1/24")
	if err := netlink.AddrAdd(hostLink, &netlink.Addr{
		IPNet: &net.IPNet{IP: hostIP, Mask: hostNet.Mask},
	}); err != nil {
		return fmt.Errorf("failed to add host IP address: %w", err)
	}

	if err := netlink.LinkSetUp(hostLink); err != nil {
		return fmt.Errorf("failed to bring host link up: %w", err)
	}

	// Move the peer interface into the Discord namespace
	peerLink, err := netlink.LinkByName(n.vethPeerNs)
	if err != nil {
		return fmt.Errorf("failed to get peer link: %w", err)
	}

	if err := netlink.LinkSetNsFd(peerLink, int(n.nsHandle)); err != nil {
		return fmt.Errorf("failed to move peer link into namespace: %w", err)
	}

	// Configure the namespace-side interface (must lock OS thread for netns switch)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := netns.Set(n.nsHandle); err != nil {
		return fmt.Errorf("failed to switch to network namespace: %w", err)
	}
	defer netns.Set(n.hostNs)

	nsLink, err := netlink.LinkByName(n.vethPeerNs)
	if err != nil {
		return fmt.Errorf("failed to get peer link inside namespace: %w", err)
	}

	nsIP, nsNet, _ := net.ParseCIDR("192.168.250.2/24")
	if err := netlink.AddrAdd(nsLink, &netlink.Addr{
		IPNet: &net.IPNet{IP: nsIP, Mask: nsNet.Mask},
	}); err != nil {
		return err
	}

	if err := netlink.LinkSetUp(nsLink); err != nil {
		return err
	}

	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return err
	}
	return netlink.LinkSetUp(lo)
}

// setupRouting adds the default route inside the namespace and enables NAT on the host.
// Caller must NOT hold LockOSThread — this function manages its own locking.
func (n *NetworkNamespace) setupRouting() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Switch into the Discord namespace to add the default route
	if err := netns.Set(n.nsHandle); err != nil {
		return err
	}

	gw := net.ParseIP("192.168.250.1")
	if err := netlink.RouteAdd(&netlink.Route{
		Dst: nil,
		Gw:  gw,
	}); err != nil {
		return fmt.Errorf("failed to add default route: %w", err)
	}

	// Return to host namespace before touching host-level settings
	if err := netns.Set(n.hostNs); err != nil {
		return err
	}

	if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644); err != nil {
		return fmt.Errorf("failed to enable ip_forward: %w", err)
	}

	// setupNAT runs inside this locked thread — do NOT lock again inside it
	return n.setupNAT()
}

// setupNAT adds iptables MASQUERADE rules on the host.
// Must be called from a thread that already holds LockOSThread and is in the host namespace.
func (n *NetworkNamespace) setupNAT() error {
	// No LockOSThread here — we inherit the lock from setupRouting()
	defaultIface, err := getDefaultInterface()
	if err != nil {
		return fmt.Errorf("failed to detect default network interface: %w", err)
	}
	log.Printf("Detected default interface: %s", defaultIface)

	rules := [][]string{
		{"iptables", "-A", "FORWARD", "-i", n.vethHost, "-j", "ACCEPT"},
		{"iptables", "-A", "FORWARD", "-o", n.vethHost, "-j", "ACCEPT"},
		{"iptables", "-t", "nat", "-A", "POSTROUTING",
			"-s", "192.168.250.0/24",
			"-o", defaultIface,
			"-j", "MASQUERADE"},
	}

	for _, rule := range rules {
		out, err := exec.Command(rule[0], rule[1:]...).CombinedOutput()
		if err != nil && !strings.Contains(string(out), "already exists") {
			log.Printf("iptables warning: %s", strings.TrimSpace(string(out)))
		}
	}

	return nil
}

// getDefaultInterface returns the name of the interface used for the default route.
// It tries netlink first, then falls back to parsing `ip route show default`.
func getDefaultInterface() (string, error) {
	// Method 1: netlink
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err == nil {
		for _, route := range routes {
			if route.Dst == nil && route.Gw != nil {
				link, err := netlink.LinkByIndex(route.LinkIndex)
				if err != nil {
					continue
				}
				return link.Attrs().Name, nil
			}
		}
	}

	// Method 2: parse `ip route show default`
	// Output format: "default via 192.168.1.1 dev wlan0 proto dhcp ..."
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "", fmt.Errorf("ip route failed: %w", err)
	}

	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}

	return "", fmt.Errorf("no default route found")
}

// GetNsHandle returns the handle for the Discord network namespace
func (n *NetworkNamespace) GetNsHandle() netns.NsHandle {
	return n.nsHandle
}

// Cleanup removes all iptables rules, the veth pair, and the namespace
func (n *NetworkNamespace) Cleanup() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Make sure we are in the host namespace
	netns.Set(n.hostNs)

	defaultIface, _ := getDefaultInterface()

	rules := [][]string{
		{"iptables", "-D", "FORWARD", "-i", n.vethHost, "-j", "ACCEPT"},
		{"iptables", "-D", "FORWARD", "-o", n.vethHost, "-j", "ACCEPT"},
		{"iptables", "-t", "nat", "-D", "POSTROUTING",
			"-s", "192.168.250.0/24",
			"-o", defaultIface,
			"-j", "MASQUERADE"},
	}

	for _, rule := range rules {
		exec.Command(rule[0], rule[1:]...).Run()
	}

	_ = netlink.LinkDel(&netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: n.vethHost}})
	_ = netns.DeleteNamed(constants.ToolNamespace)
	n.nsHandle.Close()
	n.hostNs.Close()
}
