package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/node"
)

// `fileswap service install` runs the node as a systemd user service, so it
// starts at login (or at boot with lingering) and is restarted if it
// crashes. The packages also ship the same unit in /usr/lib/systemd/user.

const (
	serviceName   = "fileswap.service"
	serviceMarker = "# Written by `fileswap service install`"
	// exitAlreadyRunning tells systemd not to restart when another node
	// for the same identity is already online (RestartPreventExitStatus).
	exitAlreadyRunning = 4
)

func serviceUnit(exe, configDir string) string {
	return serviceMarker + `; remove it with ` + "`fileswap service remove`" + `.
[Unit]
Description=FileSwap node: receive files and pairing requests
Documentation=https://github.com/SethSterling22/SwapFile/tree/main/fileswap-cli

[Service]
Type=simple
ExecStart=` + systemdQuote(exe) + ` --config ` + systemdQuote(configDir) + ` start --foreground
Environment=FILESWAP_DAEMON=1
Restart=on-failure
RestartSec=5
RestartPreventExitStatus=` + fmt.Sprint(exitAlreadyRunning) + `

[Install]
WantedBy=default.target
`
}

// systemdQuote quotes a path for ExecStart when it has spaces.
func systemdQuote(s string) string {
	if !strings.ContainsAny(s, " \t\"\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func userUnitDir() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "systemd", "user"), nil
}

func runService(e *env, args []string) error {
	fs := newFlags("service")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &errUsage{cmd: "service", msg: t("err.args")}
	}
	switch fs.Arg(0) {
	case "install":
		return serviceInstall(e)
	case "remove", "uninstall":
		return serviceRemove(e)
	default:
		return &errUsage{cmd: "service", msg: t("err.args")}
	}
}

func systemctl(e *env, args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	cmd.Stdout, cmd.Stderr = e.out, e.errOut
	return cmd.Run()
}

func serviceInstall(e *env) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New(t("service.noSystemd"))
	}
	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	configDir, err := filepath.Abs(e.dir)
	if err != nil {
		return err
	}
	dir, err := userUnitDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, serviceName)
	if err := os.WriteFile(path, []byte(serviceUnit(exe, configDir)), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(e.out, fmt.Sprintf(t("service.wrote"), short(path)))

	// A node started with `fileswap start` would make the service exit
	// "already running": hand over to systemd.
	if _, err := node.Call(context.Background(), node.SocketPath(id.ID()), node.Request{Cmd: "stop"}); err == nil {
		fmt.Fprintln(e.out, t("service.stoppedManual"))
	}
	if err := systemctl(e, "daemon-reload"); err != nil {
		return err
	}
	if err := systemctl(e, "enable", "--now", serviceName); err != nil {
		return err
	}
	fmt.Fprintln(e.out)
	fmt.Fprintln(e.out, t("service.installed"))
	if !lingering() {
		u := os.Getenv("USER")
		if cur, err := user.Current(); err == nil {
			u = cur.Username
		}
		fmt.Fprintln(e.out, fmt.Sprintf(t("service.linger"), u))
	}
	return nil
}

// lingering reports whether the user manager runs without a login session
// (so the service also starts at boot).
func lingering() bool {
	u, err := user.Current()
	if err != nil {
		return false
	}
	out, err := exec.Command("loginctl", "show-user", u.Username, "--property=Linger").Output()
	return err == nil && strings.TrimSpace(string(out)) == "Linger=yes"
}

func serviceRemove(e *env) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New(t("service.noSystemd"))
	}
	dir, err := userUnitDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, serviceName)
	_ = systemctl(e, "disable", "--now", serviceName)
	if b, err := os.ReadFile(path); err == nil {
		if !strings.HasPrefix(string(b), serviceMarker) {
			return fmt.Errorf(t("service.notOurs"), short(path))
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	_ = systemctl(e, "daemon-reload")
	fmt.Fprintln(e.out, t("service.removed"))
	return nil
}
