package process

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/MostafaSensei106/Discord-Drover-Linux/internal/constants"
	"github.com/vishvananda/netns"
)

// Launcher is responsible for launching Discord inside the network namespace
type Launcher struct {
	discordPath string
	discordArgs []string
	nsHandle    netns.NsHandle
}

// New creates a new Launcher instance
func New(path string, args []string, ns netns.NsHandle) *Launcher {
	return &Launcher{
		discordPath: path,
		discordArgs: args,
		nsHandle:    ns,
	}
}

// Launch runs Discord inside the namespace as the regular (non-root) user
func (l *Launcher) Launch() (*os.Process, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save current namespace and restore it on exit
	currentNs, err := netns.Get()
	if err != nil {
		return nil, fmt.Errorf("failed to get current namespace: %w", err)
	}
	defer currentNs.Close()
	defer netns.Set(currentNs)

	// Switch into the Discord network namespace
	if err := netns.Set(l.nsHandle); err != nil {
		return nil, fmt.Errorf("failed to switch to Discord namespace: %w", err)
	}

	// Locate the Discord executable
	if err := l.findDiscord(); err != nil {
		return nil, err
	}

	// Build Discord args based on the detected display server
	discordArgs := l.buildDiscordArgs()

	credential, credErr := getSudoUserCredential()
	sudoUser := os.Getenv("SUDO_USER")

	if credErr != nil {
		// Fallback: run with --no-sandbox if we cannot determine the real user
		log.Printf("Warning: could not determine real user, running Discord with --no-sandbox: %v", credErr)
		args := append([]string{"--no-sandbox"}, discordArgs...)
		cmd := exec.Command(l.discordPath, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("failed to launch Discord: %w", err)
		}
		log.Printf("Discord started with PID: %d", cmd.Process.Pid)
		return cmd.Process, nil
	}

	cmd := exec.Command(l.discordPath, discordArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = getUserEnv(sudoUser)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: credential,
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to launch Discord: %w", err)
	}

	log.Printf("Discord started with PID: %d (UID: %d)", cmd.Process.Pid, credential.Uid)
	return cmd.Process, nil
}

// buildDiscordArgs returns the correct Electron flags based on the active display server
func (l *Launcher) buildDiscordArgs() []string {
	args := make([]string, 0)
	args = append(args, l.discordArgs...)

	xdgSession := os.Getenv("XDG_SESSION_TYPE")
	waylandDisplay := os.Getenv("WAYLAND_DISPLAY")

	if xdgSession == "wayland" || waylandDisplay != "" {
		log.Printf("Detected Wayland (Session: %s, Display: %s), running in Wayland mode", xdgSession, waylandDisplay)
		args = append(args,
			"--ozone-platform-hint=auto",
			"--enable-features=UseOzonePlatform,WaylandWindowDecorations",
			"--ozone-platform=wayland",
			"--enable-wayland-ime",
		)
	} else {
		log.Printf("Detected X11 or fallback (Session: %s, DISPLAY: %s), running in X11 mode", xdgSession, os.Getenv("DISPLAY"))
	}

	return args
}

// findDiscord searches common paths for the Discord executable
func (l *Launcher) findDiscord() error {
	if _, err := os.Stat(l.discordPath); err == nil {
		return nil
	}

	candidates := []string{
		constants.DiscordPath,
		"/opt/discord/Discord",
		"/usr/share/discord/Discord",
		"/usr/lib/discord/Discord",
		os.ExpandEnv("$HOME/.local/share/discord/Discord"),
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			l.discordPath = p
			log.Printf("Found Discord at: %s", p)
			return nil
		}
	}

	return fmt.Errorf("Discord executable not found in any known location")
}

// getSudoUserCredential returns the UID/GID of the user who invoked sudo
func getSudoUserCredential() (*syscall.Credential, error) {
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" {
		return nil, fmt.Errorf("SUDO_USER environment variable not set")
	}

	u, err := user.Lookup(sudoUser)
	if err != nil {
		return nil, fmt.Errorf("failed to look up user '%s': %w", sudoUser, err)
	}

	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	return &syscall.Credential{
		Uid: uint32(uid),
		Gid: uint32(gid),
	}, nil
}

// getUserEnv builds a clean environment for the real user,
// replacing root-specific paths with the user's actual home and config directories.
func getUserEnv(sudoUser string) []string {
	// Strip sudo-specific and root-specific variables that we will replace
	stripKeys := map[string]bool{
		"SUDO_USER":                true,
		"SUDO_UID":                 true,
		"SUDO_GID":                 true,
		"SUDO_COMMAND":             true,
		"HOME":                     true,
		"XDG_CONFIG_HOME":          true,
		"XDG_CACHE_HOME":           true,
		"XDG_DATA_HOME":            true,
		"DBUS_SESSION_BUS_ADDRESS": true,
	}

	filtered := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		idx := strings.IndexByte(e, '=')
		if idx == -1 {
			continue
		}
		if !stripKeys[e[:idx]] {
			filtered = append(filtered, e)
		}
	}

	// Look up the real user's info
	u, err := user.Lookup(sudoUser)
	if err != nil {
		log.Printf("Warning: failed to look up user '%s': %v", sudoUser, err)
		return filtered
	}

	// Set user-specific base variables
	filtered = append(filtered,
		"HOME="+u.HomeDir,
		"USER="+u.Username,
		"LOGNAME="+u.Username,
		"XDG_RUNTIME_DIR=/run/user/"+u.Uid,
		"XDG_CONFIG_HOME="+u.HomeDir+"/.config",
		"XDG_CACHE_HOME="+u.HomeDir+"/.cache",
		"XDG_DATA_HOME="+u.HomeDir+"/.local/share",
	)

	// Set display-server-specific variables
	waylandDisplay := os.Getenv("WAYLAND_DISPLAY")
	x11Display := os.Getenv("DISPLAY")

	if waylandDisplay != "" {
		filtered = append(filtered,
			"WAYLAND_DISPLAY="+waylandDisplay,
			"XDG_SESSION_TYPE=wayland",
		)
		// Keep DISPLAY as a fallback for XWayland apps
		if x11Display != "" {
			filtered = append(filtered, "DISPLAY="+x11Display)
		}
	} else if x11Display != "" {
		filtered = append(filtered,
			"DISPLAY="+x11Display,
			"XDG_SESSION_TYPE=x11",
		)
		// Set XAUTHORITY so the user can connect to the X server
		if xauth := os.Getenv("XAUTHORITY"); xauth != "" {
			filtered = append(filtered, "XAUTHORITY="+xauth)
		} else {
			filtered = append(filtered, "XAUTHORITY="+u.HomeDir+"/.Xauthority")
		}
	} else {
		// Last resort fallback
		filtered = append(filtered, "DISPLAY=:0")
	}

	// Set the D-Bus session address for the real user
	if dbusAddr := getDBusAddress(u.Uid); dbusAddr != "" {
		filtered = append(filtered, "DBUS_SESSION_BUS_ADDRESS="+dbusAddr)
	}

	// Preserve desktop environment hints
	for _, key := range []string{
		"XDG_CURRENT_DESKTOP",
		"DESKTOP_SESSION",
		"XDG_SESSION_DESKTOP",
	} {
		if val := os.Getenv(key); val != "" {
			filtered = append(filtered, key+"="+val)
		}
	}

	return filtered
}

// getDBusAddress resolves the D-Bus session bus address for the given UID.
// It tries the environment variable first, then well-known socket paths.
func getDBusAddress(uid string) string {
	// Try the inherited environment variable first
	if addr := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); addr != "" {
		return addr
	}

	// Most modern systemd-based distros expose the bus here
	if path := fmt.Sprintf("/run/user/%s/bus", uid); fileExists(path) {
		return "unix:path=" + path
	}

	// Older fallback location
	if path := fmt.Sprintf("/run/user/%s/dbus/user_bus_socket", uid); fileExists(path) {
		return "unix:path=" + path
	}

	return ""
}

// fileExists returns true if the given path exists on disk
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// AllowX11Access runs xhost to grant the sudo user access to the X11 display.
// This is a no-op on Wayland or when no display is detected.
func AllowX11Access(sudoUser string) error {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return nil // Wayland does not require xhost
	}
	if os.Getenv("DISPLAY") == "" {
		return nil // No X11 display available
	}

	cmd := exec.Command("xhost", fmt.Sprintf("+SI:localuser:%s", sudoUser))
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("xhost failed: %s", string(out))
	}

	log.Printf("X11 access granted for user: %s", sudoUser)
	return nil
}
