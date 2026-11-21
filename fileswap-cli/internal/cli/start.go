package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/identity"
	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/node"
	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/transfer"
)

// `start` puts the node online in the background (detached from the
// terminal, logging to a file); `start -f` and `listen` run it in the
// foreground with the events on screen.

func runStart(e *env, args []string) error { return start(e, "start", args, false) }

// runListen is `start --foreground`; `listen -d` still runs in the background.
func runListen(e *env, args []string) error { return start(e, "listen", args, true) }

func start(e *env, cmdName string, args []string, foreground bool) error {
	fs := newFlags(cmdName)
	fg := fs.Bool("f", foreground, "")
	fs.BoolVar(fg, "foreground", foreground, "")
	daemon := fs.Bool("d", false, "")
	fs.BoolVar(daemon, "daemon", false, "")
	dirFlag := fs.String("dir", "", "")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &errUsage{cmd: cmdName, msg: t("err.args")}
	}
	if *daemon {
		*fg = false
	}
	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	inbox, err := id.Inbox()
	if *dirFlag != "" {
		inbox, err = resolveDir(*dirFlag)
	}
	if err != nil {
		return err
	}
	if err := ensureInbox(inbox); err != nil {
		return err
	}
	transfer.CleanStale(inbox)

	// The detached child runs with FILESWAP_DAEMON=1 and must not fork again.
	if os.Getenv("FILESWAP_DAEMON") != "" || *fg {
		return runForeground(e, id, inbox)
	}
	return startDaemon(e, id, inbox)
}

func runForeground(e *env, id *identity.Identity, inbox string) error {
	daemon := os.Getenv("FILESWAP_DAEMON") != ""
	interactive := !daemon && isTerminal(os.Stdin)
	logf := func(event string, a ...any) {
		switch event {
		case "receiveFailed", "receiveFailedFile", "sendFailed":
			if r, ok := a[len(a)-1].(string); ok {
				a[len(a)-1] = reasonMsg(r)
			}
		}
		fmt.Fprintf(e.out, "%s  %s\n", time.Now().Format("15:04:05"), tf("log."+event, a...))
	}
	opts := node.Options{Dir: e.dir, Version: e.version, HandleRequests: true, InboxDir: inbox, Log: logf}
	if interactive {
		opts.Decide = newPrompter(e).decide
	}
	n, err := node.New(opts)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ln, err := n.ListenControl(ctx)
	if err != nil {
		return nodeErr(err, id)
	}
	go n.ServeControl(ctx, ln, cancel)

	if daemon {
		// The log gets one line per start; the terminal already showed the details.
		logf("started", id.Name, id.ID(), inbox)
	} else {
		s := newStyle(e)
		fmt.Fprintf(e.out, "%s  %s\n\n", s.bold(id.Name), s.dim(t("start.fgTitle")))
		printCode(e, s, id.ID())
		fmt.Fprintln(e.out)
		printRows(e, [][2]string{{t("ui.server"), id.Server}, {t("ui.folder"), short(inbox)}})
		fmt.Fprintln(e.out)
		fmt.Fprintln(e.out, "  "+s.dim(t("start.fgHint")))
		fmt.Fprintln(e.out)
	}
	err = n.Run(ctx)
	logf("stopped")
	if ctx.Err() != nil {
		return nil
	}
	return nodeErr(err, id)
}

// startDaemon re-runs fileswap detached from the terminal, logging to a
// file, and shows the status once it is up.
func startDaemon(e *env, id *identity.Identity, inbox string) error {
	s := newStyle(e)
	path := node.SocketPath(id.ID())
	logPath, err := logFile()
	if err != nil {
		return err
	}
	if resp, err := node.Call(context.Background(), path, node.Request{Cmd: "status"}); err == nil && resp.Status != nil {
		printStatus(e, s, resp.Status, logPath)
		fmt.Fprintln(e.out)
		fmt.Fprintln(e.out, "  "+t("start.already"))
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := filepath.Abs(e.dir)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	cmd := exec.Command(exe, "--config", dir, "start", "--dir", inbox)
	cmd.Env = append(os.Environ(), "FILESWAP_DAEMON=1")
	cmd.Stdout, cmd.Stderr = f, f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()

	// Wait for the control socket, then briefly for the server connection,
	// so the status shown is the real one.
	var st *node.Status
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		select {
		case <-exited:
			return fmt.Errorf(t("start.failed"), logPath, logTail(logPath))
		case <-time.After(100 * time.Millisecond):
		}
		resp, err := node.Call(context.Background(), path, node.Request{Cmd: "status"})
		if err != nil || resp.Status == nil {
			continue
		}
		st = resp.Status
		if st.Online || time.Until(deadline) < 5*time.Second {
			break
		}
	}
	if st == nil {
		return fmt.Errorf(t("start.failed"), logPath, logTail(logPath))
	}
	printStatus(e, s, st, logPath)
	fmt.Fprintln(e.out)
	if !st.Online {
		fmt.Fprintln(e.out, "  "+t("start.stillConnecting"))
	}
	fmt.Fprintln(e.out, "  "+t("start.hint"))
	return nil
}

// logFile is $XDG_STATE_HOME/fileswap/listen.log, else ~/.local/state/fileswap/listen.log.
func logFile() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	dir = filepath.Join(dir, "fileswap")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "listen.log"), nil
}

// logTail returns the last lines of the background log, to show why it failed.
func logTail(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, "\n")
}
