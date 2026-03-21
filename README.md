<h1 align="center">Discord Drover</h1>
<p align="center">
  <img src="https://socialify.git.ci/MostafaSensei106/Discord-Drover-Linux/image?custom_language=Go&font=KoHo&language=1&logo=https%3A%2F%2Favatars.githubusercontent.com%2Fu%2F138288138%3Fv%3D4&name=1&owner=1&pattern=Floating+Cogs&theme=Light" alt="Discord Drover Banner">
</p>

<p align="center">
  <strong>A high-performance DPI bypass and network isolation tool for Discord on Linux.</strong><br>
  Fast. Secure. Stealthy. Voice and Video calls that just work.
</p>

<p align="center">
  <a href="#about">About</a> •
  <a href="#features">Features</a> •
  <a href="#installation">Installation</a> •
  <a href="#configuration">Configuration</a> •
  <a href="#technologies">Technologies</a> •
  <a href="#contributing">Contributing</a> •
  <a href="#license">License</a>
</p>

---

## About

Welcome to **Discord Drover** — a specialized utility designed to bypass Deep Packet Inspection (DPI) and regional blocks affecting Discord on Linux systems. 

Unlike generic VPNs, Discord Drover creates a dedicated Linux Network Namespace for Discord, isolating its traffic and applying advanced obfuscation strategies specifically to the UDP packets used for voice and video. It provides a transparent bridge between isolated networking and your system's proxy, ensuring a seamless and high-performance experience.

---

## Features

### 🌟 Core Functionality

- **Network Namespace Isolation**: Runs Discord in a completely separate network stack to prevent leaks and ensure clean routing.
- **Advanced DPI Bypass**: Intercepts UDP traffic via `NFQUEUE` to apply fragmentation and padding strategies that evade packet inspection.
- **Transparent TCP Proxying**: Automatically tunnels Discord's TCP traffic through SOCKS5 or HTTP proxies without system-wide configuration.
- **Voice & Video Optimization**: Specifically handles RTP/UDP traffic to ensure stable calls even in restricted network environments.
- **Automatic Cleanup**: Gracefully restores system iptables rules and network interfaces upon termination.

### 🛠️ Advanced Capabilities

- **UDP Obfuscation Strategies**:
  - **Fragmentation**: Randomly splits UDP payloads to break signature matching.
  - **Junk Padding**: Adds randomized RTP extensions (padding) to disguise packet length.
  - **Combined Mode**: Applies both strategies for maximum stealth.
- **Smart Launcher**: 
  - Automatically detects Discord installation paths (Flatpak, Opt, etc.).
  - Handles **Wayland** and **X11** display server environments.
  - Runs Discord as the original user (not root) to preserve themes and settings.
- **Direct Mode**: Option to run with only UDP bypass, skipping the proxy for lower latency when only voice/video is blocked.

### 🛡️ Security & Reliability

- **Root Privilege Management**: Requires root for setup but drops privileges when launching the Discord process.
- **Path Validation**: Intelligent searching for the Discord executable across common Linux directories.
- **Environment Cleaning**: Strips root-specific environment variables to ensure Discord runs in a clean user context.

---

## Installation

### ⚠️ IMPORTANT: Requirements

Discord Drover requires several Linux kernel features and system utilities:
- **Root Privileges**: Required to manage network namespaces and `iptables`.
- **iptables**: Used for packet redirection.
- **NFQUEUE support**: Your kernel must support Netfilter Queue (`libnetfilter-queue`).

#### 🔧 Installing Dependencies (Debian/Ubuntu)

```bash
sudo apt update
sudo apt install libnetfilter-queue-dev iptables iproute2
```

---

## 🏗️ Build from Source

Ensure you have `Go` (1.21 or later) installed.

```bash
git clone --depth 1 https://github.com/MostafaSensei106/Discord-Drover-Linux.git
cd Discord-Drover-Linux
make
```

This will compile the binary and place it in the project root.

---

## 🚀 Quick Start

```bash
# Start Discord with the default config (bypass.ini)
sudo ./discord-drover

# Start in Direct Mode (UDP bypass only, no proxy)
sudo ./discord-drover --direct

# Use a custom config file and Discord path
sudo ./discord-drover --config ./my_config.ini --discord /usr/bin/discord-canary
```

---

## Configuration

Discord Drover uses an INI config file (default: `bypass.ini`).

### 🧾 Example Config:

```ini
[bypass]
# Your SOCKS5 or HTTP proxy URL
proxy = socks5://127.0.0.1:1080

# If true, only UDP obfuscation is applied; TCP bypasses the proxy
direct_mode = false

# Custom path to Discord executable (auto-detected if empty)
discord_path = /usr/bin/discord

# Enable UDP fragmentation (part of DPI bypass)
udp_fragmentation = true

# Custom Fake TTL value for packets
fake_ttl = 5
```

---

## Technologies

| Technology          | Description                                                                                                 |
| ------------------- | ----------------------------------------------------------------------------------------------------------- |
| 🧠 **Golang**       | [go.dev](https://go.dev) — Core language for high-performance networking                                     |
| 📦 **Gopacket**     | [google/gopacket](https://github.com/google/gopacket) — Packet construction and deconstruction               |
| 🌐 **Netlink**      | [vishvananda/netlink](https://github.com/vishvananda/netlink) — Linux network interface and route management|
| 🔒 **NFQueue**      | [florianl/go-nfqueue](https://github.com/florianl/go-nfqueue) — Interaction with Netfilter Queue            |
| ⚙️ **Netns**        | [vishvananda/netns](https://github.com/vishvananda/netns) — Network namespace manipulation                  |
| 📜 **Ini.v1**       | [gopkg.in/ini.v1](https://pkg.go.dev/gopkg.in/ini.v1) — Robust INI configuration parsing                     |

---

## Contributing

Contributions are welcome! Whether it's a new obfuscation strategy or a fix for a specific distribution, feel free to help:

1.  Fork the repository.
2.  Create a new branch: `git checkout -b feature/AmazingFeature`.
3.  Commit your changes: `git commit -m "Add amazing feature"`.
4.  Push to the branch: `git push origin feature/AmazingFeature`.
5.  Open a pull request.

---

## License

This project is licensed under the **GPL-3.0 License**.
See the [LICENSE](LICENSE) file for full details.

<p align="center">
  Made with ❤️ by <a href="https://github.com/MostafaSensei106">MostafaSensei106</a>
</p>
