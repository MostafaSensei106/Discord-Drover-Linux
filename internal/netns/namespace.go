package netns

import (
	"fmt"
	"net"
	"os"
	"runtime"

	"github.com/MostafaSensei106/Discord-Drover-Linux/internal/constants"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

type NetworkNamespace struct {
	nsHandle   netns.NsHandle
	hostNs     netns.NsHandle
	vethHost   string
	vethPeerNs string
}

func Setup() (*NetworkNamespace, error) {
	runtime.LockOSThread()

	defer runtime.UnlockOSThread()

	hostNs, err := netns.Get()
	if err != nil {
		return nil, fmt.Errorf("failed to get host network namespace: %w", err)
	}

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

func (n *NetworkNamespace) setupVethPair() error {
	_ = netlink.LinkDel(&netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: n.vethHost}})

	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name: n.vethHost,
		},
		PeerName: n.vethPeerNs,
	}

	if err := netlink.LinkAdd(veth); err != nil {
		return fmt.Errorf("failed to add veth pair: %w", err)
	}

	hostLink, err := netlink.LinkByName(n.vethHost)
	if err != nil {
		return fmt.Errorf("failed to get host link: %w", err)
	}

	hostIP, hostNet, _ := net.ParseCIDR("192.168.250.1/24")
	if err := netlink.AddrAdd(hostLink, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   hostIP,
			Mask: hostNet.Mask,
		},
	}); err != nil {
		return fmt.Errorf("failed to add host IP address: %w", err)
	}

	if err := netlink.LinkSetUp(hostLink); err != nil {
		return fmt.Errorf("failed to set host link up: %w", err)
	}

	peerLink, err := netlink.LinkByName(n.vethPeerNs)
	if err != nil {
		return fmt.Errorf("failed to get peer link: %w", err)
	}

	if err := netlink.LinkSetNsFd(peerLink, int(n.nsHandle)); err != nil {
		return fmt.Errorf("failed to set peer link up: %w", err)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := netns.Set(n.nsHandle); err != nil {
		return fmt.Errorf("failed to switch to network namespace: %w", err)
	}

	defer netns.Set(n.hostNs)

	nsLink, err := netlink.LinkByName(n.vethPeerNs)
	if err != nil {
		return fmt.Errorf("failed to get peer link: %w", err)
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

func (n *NetworkNamespace) setupRouting() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := netns.Set(n.nsHandle); err != nil {
		return err
	}

	gw := net.ParseIP("192.168.250.1")
	defaultRoute := &netlink.Route{
		Dst: nil, // default route
		Gw:  gw,
	}

	if err := netlink.RouteAdd(defaultRoute); err != nil {
		return fmt.Errorf("faild to add default route: %w", err)
	}

	if err := netns.Set(n.hostNs); err != nil {
		return err
	}

	return os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644)
}

func (n *NetworkNamespace) GetNsHandle() netns.NsHandle {
	return n.nsHandle
}

func (n *NetworkNamespace) Cleanup() {
	_ = netlink.LinkDel(&netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: n.vethHost}})
	_ = netns.DeleteNamed(constants.ToolNamespace)
	n.nsHandle.Close()
	n.hostNs.Close()
}
