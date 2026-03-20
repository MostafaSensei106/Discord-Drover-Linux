package process

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"github.com/MostafaSensei106/Discord-Drover-Linux/internal/constants"
	"github.com/vishvananda/netns"
)

type Launcher struct {
	discordPath string
	discordArgs []string
	nsHandle    netns.NsHandle
}

func New(path string, args []string, ns netns.NsHandle) *Launcher {
	return &Launcher{
		discordPath: path,
		discordArgs: args,
		nsHandle:    ns,
	}
}

func (l *Launcher) Launch() (*os.Process, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	currentNs, err := netns.Get()
	if err != nil {
		return nil, fmt.Errorf("failed to get current network namespace: %w", err)
	}
	defer currentNs.Close()
	defer netns.Set(currentNs)

	if err := netns.Set(l.nsHandle); err != nil {
		return nil, fmt.Errorf("failed to switch to network namespace: %w", err)
	}

	if _, err := os.Stat(l.discordPath); os.IsNotExist(err) {
		paths := []string{
			constants.DiscordPath,
			"/opt/discord/Discord",
			"/usr/share/discord/Discord",
			os.ExpandEnv("$HOME/.local/share/discord/Discord"),
		}

		found := false
		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				l.discordPath = path
				found = true
				break
			}
		}

		if !found {
			return nil, fmt.Errorf("discord executable not found")
		}

	}

	cmd := exec.Command(l.discordPath, l.discordArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	cmd.Env = os.Environ()

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: 0,
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start discord: %w", err)
	}

	log.Printf("Discord started with PID: %d", cmd.Process.Pid)
	return cmd.Process, nil
}
