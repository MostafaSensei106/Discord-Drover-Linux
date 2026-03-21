package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/MostafaSensei106/Discord-Drover-Linux/internal/config"
	"github.com/MostafaSensei106/Discord-Drover-Linux/internal/constants"
	"github.com/MostafaSensei106/Discord-Drover-Linux/internal/firewall"
	"github.com/MostafaSensei106/Discord-Drover-Linux/internal/netns"
	"github.com/MostafaSensei106/Discord-Drover-Linux/internal/packet"
	"github.com/MostafaSensei106/Discord-Drover-Linux/internal/process"
)

func Execute() {

	if os.Geteuid() != 0 {
		log.Fatal("This program requires root privileges. Please run with sudo.")
	}

	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if err := process.AllowX11Access(sudoUser); err != nil {
			log.Printf("xhost warning: %v", err)
		}
	}

	configPath := flag.String("config", "bypass.ini", "Path to config file")
	directMode := flag.Bool("direct", false, "Direct mode (UDP bypass only, no proxy)")
	discordPath := flag.String("discord", "", "Path to Discord executable")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("Warning: Failed to load config: %v, using default settings", err)
		cfg = &config.Config{
			NFQueueNum:       1,
			UDPFragmentation: true,
			FakeTTL:          5,
			DiscordPath:      constants.DiscordPath,
		}
	}

	if *directMode {
		cfg.DirectMode = true
		cfg.ProxyURL = ""
	}

	if *discordPath != "" {
		cfg.DiscordPath = *discordPath
	}

	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║     Discord Drover Bypass for Linux  ║")
	fmt.Println("╚══════════════════════════════════════╝")

	if cfg.DirectMode || cfg.ProxyURL == "" {
		fmt.Println("📡 Mode: Direct (UDP Bypass only)")
	} else {
		fmt.Printf("📡 Mode: Proxy (%s)\n", cfg.ProxyURL)
	}
	fmt.Println("────────────────────────────────────────")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Setup Network Namespace
	fmt.Print("⚙️ Creating Network Namespace... ")
	ns, err := netns.Setup()
	if err != nil {
		log.Fatalf("Failed: %v", err)
	}
	defer ns.Cleanup()
	fmt.Println("✅")

	// 2. Setup Transparent Proxy for TCP (if not in direct mode)
	var proxyPort int
	if !cfg.DirectMode && cfg.ProxyURL != "" {
		proxyPort, err = setupTransparentProxy(ctx, cfg.ProxyURL)
		if err != nil {
			log.Fatalf("Failed to setup Proxy: %v", err)
		}
		fmt.Printf("🔀 Transparent Proxy on port %d\n", proxyPort)
	}

	// 3. Apply iptables rules
	fmt.Print("🔥 Applying iptables rules... ")
	fw := firewall.New(proxyPort, cfg.NFQueueNum, constants.ToolNamespace)
	if err := fw.Apply(cfg.DirectMode); err != nil {
		log.Fatalf("Failed: %v", err)
	}
	defer fw.Cleanup()
	fmt.Println("✅")

	// 4. Start NFQUEUE listener for UDP
	fmt.Print("📦 Starting NFQUEUE listener... ")
	strategy := packet.StrategyPadding
	if cfg.UDPFragmentation {
		strategy = packet.StrategyCombined
	}

	handler := packet.New(cfg.NFQueueNum, strategy)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := handler.Start(ctx); err != nil && ctx.Err() == nil {
			log.Printf("Error in NFQUEUE handler: %v", err)
		}
	}()
	fmt.Println("✅")

	// 5. Launch Discord
	fmt.Printf("🚀 Launching Discord (%s)...\n", cfg.DiscordPath)
	launcher := process.New(cfg.DiscordPath, cfg.DiscordArgs, ns.GetNsHandle())
	discordProcess, err := launcher.Launch()
	if err != nil {
		log.Fatalf("Failed to launch Discord: %v", err)
	}

	fmt.Println("────────────────────────────────────────")
	fmt.Println("✅ Everything is running! Discord is now using DPI bypass")
	fmt.Println("   Press Ctrl+C to stop")
	fmt.Println("────────────────────────────────────────")

	// Wait for Ctrl+C or Discord exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	discordDone := make(chan error, 1)
	go func() {
		_, err := discordProcess.Wait()
		discordDone <- err
	}()

	select {
	case <-sigChan:
		fmt.Println("\n⏹️  Stopping...")
		discordProcess.Kill()
	case <-discordDone:
		fmt.Println("\n✅ Discord closed")
	}

	// Cleanup
	cancel()
	wg.Wait()
	fmt.Println("✅ Cleanup successful")
}

// setupTransparentProxy starts a simple Transparent Proxy to forward TCP to SOCKS5/HTTP proxy
func setupTransparentProxy(ctx context.Context, proxyURL string) (int, error) {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return 0, fmt.Errorf("invalid proxy URL: %w", err)
	}

	// Listen on a random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}

	port := listener.Addr().(*net.TCPAddr).Port

	go runTransparentProxy(ctx, listener, parsed)

	return port, nil
}

// runTransparentProxy runs the transparent proxy loop
func runTransparentProxy(ctx context.Context, listener net.Listener, proxyURL *url.URL) {
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}

		go handleTCPConnection(conn, proxyURL)
	}
}

func handleTCPConnection(clientConn net.Conn, proxyURL *url.URL) {
	defer clientConn.Close()

	// Extract original destination
	dest, err := getOriginalDest(clientConn)
	if err != nil {
		log.Printf("Failed to get destination: %v", err)
		return
	}

	// Connect to proxy
	var proxyConn net.Conn

	switch proxyURL.Scheme {
	case "socks5":
		proxyConn, err = connectViaSOCKS5(proxyURL, dest)
	case "http":
		proxyConn, err = connectViaHTTPProxy(proxyURL, dest)
	default:
		log.Printf("Unsupported proxy type: %s", proxyURL.Scheme)
		return
	}

	if err != nil {
		log.Printf("Failed to connect to proxy: %v", err)
		return
	}
	defer proxyConn.Close()

	// Start relay
	relay(clientConn, proxyConn)
}

// getOriginalDest retrieves the original IP:Port of the TCP connection
func getOriginalDest(conn net.Conn) (string, error) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return "", fmt.Errorf("not a TCP connection")
	}

	file, err := tcpConn.File()
	if err != nil {
		return "", err
	}
	defer file.Close()

	// SO_ORIGINAL_DST to get the original destination before REDIRECT
	addr, err := syscall.GetsockoptIPv6Mreq(int(file.Fd()), syscall.IPPROTO_IP, 80) // SO_ORIGINAL_DST = 80
	if err != nil {
		return "", fmt.Errorf("SO_ORIGINAL_DST failed: %w", err)
	}

	ip := net.IP(addr.Multiaddr[4:8])
	port := uint16(addr.Multiaddr[2])<<8 | uint16(addr.Multiaddr[3])
	return fmt.Sprintf("%s:%d", ip.String(), port), nil
}

func connectViaSOCKS5(proxyURL *url.URL, dest string) (net.Conn, error) {
	conn, err := net.Dial("tcp", proxyURL.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SOCKS5 server: %w", err)
	}

	// ---- Phase 1: Greeting ----
	// Tell the server we are using SOCKS5 and specify supported auth methods
	hasAuth := proxyURL.User != nil && proxyURL.User.Username() != ""

	var greeting []byte
	if hasAuth {
		// Two methods: NoAuth (0x00) and Username/Password (0x02)
		greeting = []byte{0x05, 0x02, 0x00, 0x02}
	} else {
		// One method: NoAuth
		greeting = []byte{0x05, 0x01, 0x00}
	}

	if _, err := conn.Write(greeting); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send greeting: %w", err)
	}

	// Server responds with: [Version=0x05, ChosenMethod]
	serverChoice := make([]byte, 2)
	if _, err := io.ReadFull(conn, serverChoice); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read server choice: %w", err)
	}

	if serverChoice[0] != 0x05 {
		conn.Close()
		return nil, fmt.Errorf("invalid SOCKS version: %d", serverChoice[0])
	}

	// ---- Phase 2: Authentication (if required) ----
	switch serverChoice[1] {
	case 0x00:
		// No authentication required
	case 0x02:
		// Username/Password authentication (RFC 1929)
		if !hasAuth {
			conn.Close()
			return nil, fmt.Errorf("server requires authentication but no credentials provided")
		}

		username := proxyURL.User.Username()
		password, _ := proxyURL.User.Password()

		// Build auth request: [Ver=0x01, ULen, Username, PLen, Password]
		authReq := make([]byte, 0, 3+len(username)+len(password))
		authReq = append(authReq, 0x01) // sub-negotiation version
		authReq = append(authReq, byte(len(username)))
		authReq = append(authReq, []byte(username)...)
		authReq = append(authReq, byte(len(password)))
		authReq = append(authReq, []byte(password)...)

		if _, err := conn.Write(authReq); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to send credentials: %w", err)
		}

		// Server responds with: [Ver=0x01, Status] where 0x00 = success
		authResp := make([]byte, 2)
		if _, err := io.ReadFull(conn, authResp); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to read auth response: %w", err)
		}

		if authResp[1] != 0x00 {
			conn.Close()
			return nil, fmt.Errorf("authentication failed: code %d", authResp[1])
		}

	case 0xFF:
		conn.Close()
		return nil, fmt.Errorf("server rejected all authentication methods")

	default:
		conn.Close()
		return nil, fmt.Errorf("unsupported auth method: 0x%02x", serverChoice[1])
	}

	// ---- Phase 3: Connection Request ----
	// Ask the server to connect to dest on our behalf
	host, portStr, err := net.SplitHostPort(dest)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("invalid dest format: %w", err)
	}

	portNum, err := strconv.Atoi(portStr)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	// Build the request
	// [Ver=5, Cmd=CONNECT(1), RSV=0, ATYP, DST.ADDR, DST.PORT]
	request := []byte{0x05, 0x01, 0x00}

	ip := net.ParseIP(host)
	if ip == nil {
		// Domain name (ATYP = 0x03)
		request = append(request, 0x03)
		request = append(request, byte(len(host)))
		request = append(request, []byte(host)...)
	} else if ipv4 := ip.To4(); ipv4 != nil {
		// IPv4 (ATYP = 0x01)
		request = append(request, 0x01)
		request = append(request, ipv4...)
	} else {
		// IPv6 (ATYP = 0x04)
		request = append(request, 0x04)
		request = append(request, ip.To16()...)
	}

	// Port in Big Endian
	request = append(request, byte(portNum>>8), byte(portNum&0xFF))

	if _, err := conn.Write(request); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send connection request: %w", err)
	}

	// ---- Phase 4: Read Response ----
	// Server responds with: [Ver, REP, RSV, ATYP, BND.ADDR, BND.PORT]
	respHeader := make([]byte, 4)
	if _, err := io.ReadFull(conn, respHeader); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read response header: %w", err)
	}

	if respHeader[0] != 0x05 {
		conn.Close()
		return nil, fmt.Errorf("invalid SOCKS version in response")
	}

	// Check the REP (reply code)
	if respHeader[1] != 0x00 {
		conn.Close()
		errMsg := socks5Error(respHeader[1])
		return nil, fmt.Errorf("SOCKS5 server rejected request: %s", errMsg)
	}

	// Finish reading BND.ADDR and BND.PORT (not needed but must be consumed)
	switch respHeader[3] {
	case 0x01: // IPv4
		addr := make([]byte, 4+2)
		io.ReadFull(conn, addr)
	case 0x03: // Domain
		lenBuf := make([]byte, 1)
		io.ReadFull(conn, lenBuf)
		addr := make([]byte, int(lenBuf[0])+2)
		io.ReadFull(conn, addr)
	case 0x04: // IPv6
		addr := make([]byte, 16+2)
		io.ReadFull(conn, addr)
	}

	// ✅ Connection established, conn is ready for use
	return conn, nil
}

// socks5Error converts error code to a human-readable message
func socks5Error(code byte) string {
	errors := map[byte]string{
		0x01: "General SOCKS server failure",
		0x02: "Connection not allowed by ruleset",
		0x03: "Network unreachable",
		0x04: "Host unreachable",
		0x05: "Connection refused",
		0x06: "TTL expired",
		0x07: "Command not supported",
		0x08: "Address type not supported",
	}
	if msg, ok := errors[code]; ok {
		return msg
	}
	return fmt.Sprintf("Unknown error: 0x%02x", code)
}

func connectViaHTTPProxy(proxyURL *url.URL, dest string) (net.Conn, error) {
	conn, err := net.Dial("tcp", proxyURL.Host)
	if err != nil {
		return nil, err
	}

	// HTTP CONNECT method
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", dest, dest)

	// Read response
	buf := make([]byte, 256)
	_, err = conn.Read(buf)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

func relay(conn1, conn2 net.Conn) {
	done := make(chan struct{}, 2)

	copy := func(dst, src net.Conn) {
		buf := make([]byte, 32*1024)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				dst.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}

	go copy(conn1, conn2)
	go copy(conn2, conn1)
	<-done
}
