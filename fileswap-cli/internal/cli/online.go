package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/identity"
	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/node"
)

// Commands that need the server. Each one first asks a running `listen`
// over its control socket (the server allows a single connection per
// identity); without one, it connects just for the command and disconnects.

const (
	connectTimeout = 10 * time.Second
	presenceWait   = 700 * time.Millisecond // presence messages follow "registered"
	exitNotRunning = 3                      // like `systemctl status` for an inactive unit
)

// errExit ends the command with a message on stdout and a specific exit code.
type errExit struct {
	code int
	msg  string
}

func (e *errExit) Error() string { return e.msg }

func control(ctx context.Context, id *identity.Identity, req node.Request) (*node.Response, error) {
	return node.Call(ctx, node.SocketPath(id.ID()), req)
}

// oneShot connects a temporary node and runs fn once it is online (or after
// connectTimeout, with online=false).
func oneShot(e *env, fn func(ctx context.Context, n *node.Node, online bool) error) error {
	n, err := node.New(node.Options{Dir: e.dir, Version: e.version})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- n.Run(ctx) }()

	wctx, wcancel := context.WithTimeout(ctx, connectTimeout)
	err = n.WaitOnline(wctx)
	wcancel()
	select {
	case rerr := <-runErr: // replaced right away: a listen started meanwhile
		return rerr
	default:
	}
	return fn(ctx, n, err == nil)
}

func runSync(e *env, args []string) error {
	fs := newFlags("sync")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &errUsage{cmd: "sync", msg: t("err.args")}
	}
	code := strings.ToLower(strings.TrimSpace(fs.Arg(0)))
	if !identity.IsDeviceID(code) {
		return fmt.Errorf(t("err.badCode"), fs.Arg(0))
	}
	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	switch {
	case code == id.ID():
		return nodeErr(&node.Error{Code: node.CodeSelf}, id)
	case id.HasPair(code):
		p, _ := id.Find(code)
		return nodeErr(&node.Error{Code: node.CodeAlreadyPaired, Detail: p.Name}, id)
	}
	fmt.Fprintln(e.out, fmt.Sprintf(t("sync.sent"), code))

	ctx, cancel := context.WithTimeout(context.Background(), node.SyncTimeout+10*time.Second)
	defer cancel()
	var name string
	if resp, err := control(ctx, id, node.Request{Cmd: "sync", Arg: code}); err == nil {
		name, err = resp.Name, resp.AsError()
		if err != nil {
			return nodeErr(err, id)
		}
	} else {
		err = oneShot(e, func(ctx context.Context, n *node.Node, online bool) error {
			if !online {
				return &node.Error{Code: node.CodeOffline}
			}
			sctx, scancel := context.WithTimeout(ctx, node.SyncTimeout)
			defer scancel()
			name, err = n.Sync(sctx, code)
			return err
		})
		if err != nil {
			return nodeErr(err, id)
		}
	}
	fmt.Fprintln(e.out, fmt.Sprintf(t("sync.paired"), name, code))
	return nil
}

func runPeers(e *env, args []string) error {
	fs := newFlags("peers")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &errUsage{cmd: "peers", msg: t("err.args")}
	}
	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	if len(id.Pairs) == 0 {
		fmt.Fprintln(e.out, t("peers.none"))
		return nil
	}

	ctx := context.Background()
	var st *node.Status
	if resp, err := control(ctx, id, node.Request{Cmd: "status"}); err == nil && resp.Status != nil {
		st = resp.Status
	} else {
		_ = oneShot(e, func(ctx context.Context, n *node.Node, online bool) error {
			if online {
				time.Sleep(presenceWait)
				s := n.Status()
				st = &s
			}
			return nil
		})
	}
	if st == nil {
		fmt.Fprintln(e.errOut, fmt.Sprintf(t("peers.unreachable"), id.Server))
		for _, p := range id.Pairs {
			fmt.Fprintf(e.out, "?  %s  %s\n", p.ID, p.Name)
		}
		return nil
	}
	printPeers(e, st.Peers)
	return nil
}

func printPeers(e *env, peers []node.Peer) {
	for _, p := range peers {
		mark, state := "○", t("peer.offline")
		if p.Online {
			mark, state = "●", t("peer.online")
		}
		fmt.Fprintf(e.out, "%s  %s  %-12s  %s\n", mark, p.ID, state, p.Name)
	}
}

func runUnpair(e *env, args []string) error {
	fs := newFlags("unpair")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &errUsage{cmd: "unpair", msg: t("err.args")}
	}
	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	ctx := context.Background()
	var name string
	var uerr error
	if resp, err := control(ctx, id, node.Request{Cmd: "unpair", Arg: fs.Arg(0)}); err == nil {
		name, uerr = resp.Name, resp.AsError()
	} else {
		err = oneShot(e, func(ctx context.Context, n *node.Node, online bool) error {
			p, err := n.Unpair(ctx, fs.Arg(0))
			name, uerr = p.Name, err
			return nil
		})
		if err != nil {
			return nodeErr(err, id)
		}
	}
	var ne *node.Error
	switch {
	case errors.As(uerr, &ne) && ne.Code == node.CodeOffline && name != "":
		fmt.Fprintln(e.out, fmt.Sprintf(t("unpair.localOnly"), name))
	case uerr != nil:
		return nodeErr(uerr, id)
	default:
		fmt.Fprintln(e.out, fmt.Sprintf(t("unpair.done"), name))
	}
	return nil
}

func runStatus(e *env, args []string) error {
	fs := newFlags("status")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &errUsage{cmd: "status", msg: t("err.args")}
	}
	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	resp, err := control(context.Background(), id, node.Request{Cmd: "status"})
	if err != nil || resp.Status == nil {
		return &errExit{code: exitNotRunning, msg: fmt.Sprintf(t("status.notRunning"), id.Name, id.ID())}
	}
	st := resp.Status
	state := t("status.connecting")
	if st.Online {
		state = t("status.online")
	}
	fmt.Fprintln(e.out, fmt.Sprintf(t("status.head"), state, st.Name, st.ID))
	fmt.Fprintln(e.out, fmt.Sprintf(t("status.server"), st.Server))
	fmt.Fprintln(e.out, fmt.Sprintf(t("status.pid"), st.PID, time.Since(st.Since).Round(time.Second), st.Version))
	if len(st.Peers) == 0 {
		fmt.Fprintln(e.out, t("peers.none"))
		return nil
	}
	fmt.Fprintln(e.out)
	printPeers(e, st.Peers)
	return nil
}

func runStop(e *env, args []string) error {
	fs := newFlags("stop")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &errUsage{cmd: "stop", msg: t("err.args")}
	}
	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	path := node.SocketPath(id.ID())
	if _, err := node.Call(context.Background(), path, node.Request{Cmd: "stop"}); err != nil {
		fmt.Fprintln(e.out, t("stop.notRunning"))
		return nil
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			break
		}
	}
	fmt.Fprintln(e.out, t("stop.done"))
	return nil
}

func runListen(e *env, args []string) error {
	fs := newFlags("listen")
	daemon := fs.Bool("d", false, "")
	fs.BoolVar(daemon, "daemon", false, "")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &errUsage{cmd: "listen", msg: t("err.args")}
	}
	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	if *daemon && os.Getenv("FILESWAP_DAEMON") == "" {
		return startDaemon(e, id)
	}

	interactive := os.Getenv("FILESWAP_DAEMON") == "" && isTerminal(os.Stdin)
	logf := func(event string, a ...any) {
		fmt.Fprintf(e.out, "%s  %s\n", time.Now().Format("15:04:05"), tf("log."+event, a...))
	}
	opts := node.Options{Dir: e.dir, Version: e.version, HandleRequests: true, Log: logf}
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

	fmt.Fprintln(e.out, fmt.Sprintf(t("listen.banner"), id.Name, id.ID(), id.Server))
	if interactive {
		fmt.Fprintln(e.out, t("listen.hint"))
	} else {
		fmt.Fprintln(e.out, t("listen.hintDaemon"))
	}
	err = n.Run(ctx)
	fmt.Fprintln(e.out, time.Now().Format("15:04:05")+"  "+t("listen.stopped"))
	if ctx.Err() != nil {
		return nil
	}
	return nodeErr(err, id)
}

// startDaemon re-runs `fileswap listen` detached from the terminal, logging
// to a file, and returns once its control socket answers.
func startDaemon(e *env, id *identity.Identity) error {
	path := node.SocketPath(id.ID())
	if resp, err := node.Call(context.Background(), path, node.Request{Cmd: "status"}); err == nil {
		pid := ""
		if resp.Status != nil {
			pid = fmt.Sprint(resp.Status.PID)
		}
		return nodeErr(&node.Error{Code: node.CodeAlreadyRunning, Detail: pid}, id)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := filepath.Abs(e.dir)
	if err != nil {
		return err
	}
	logPath, err := logFile()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	cmd := exec.Command(exe, "--config", dir, "listen")
	cmd.Env = append(os.Environ(), "FILESWAP_DAEMON=1")
	cmd.Stdout, cmd.Stderr = f, f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()

	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		select {
		case <-exited:
			return fmt.Errorf(t("listen.bgFailed"), logPath, logTail(logPath))
		case <-time.After(100 * time.Millisecond):
		}
		if _, err := node.Call(context.Background(), path, node.Request{Cmd: "status"}); err == nil {
			fmt.Fprintln(e.out, fmt.Sprintf(t("listen.bg"), id.Name, cmd.Process.Pid, logPath))
			return nil
		}
	}
	return fmt.Errorf(t("listen.bgFailed"), logPath, logTail(logPath))
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

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// prompter asks y/N questions on the terminal, one at a time.
type prompter struct {
	e     *env
	mu    sync.Mutex
	lines chan string
}

func newPrompter(e *env) *prompter {
	p := &prompter{e: e, lines: make(chan string)}
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			p.lines <- sc.Text()
		}
		close(p.lines)
	}()
	return p
}

func (p *prompter) decide(ctx context.Context, code, name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprint(p.e.out, fmt.Sprintf(t("prompt.ask"), name, code))
	select {
	case line, ok := <-p.lines:
		answer := strings.ToLower(strings.TrimSpace(line))
		return ok && (answer == "y" || answer == "yes" || answer == "s" || answer == "si" || answer == "sí")
	case <-ctx.Done():
		fmt.Fprintln(p.e.out)
		return false
	}
}

// nodeErr translates node error codes into the user's language.
func nodeErr(err error, id *identity.Identity) error {
	var ne *node.Error
	if !errors.As(err, &ne) {
		return err
	}
	detail := ne.Detail
	if ne.Code == node.CodeOffline && detail == "" {
		detail = id.Server
	}
	key := "nerr." + ne.Code
	if _, ok := messages["en"][key]; !ok {
		return err
	}
	return errors.New(tf(key, detail))
}

var verbRE = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

// tf formats a message, passing only as many args as it has verbs.
func tf(key string, args ...any) string {
	s := t(key)
	if n := len(verbRE.FindAllString(s, -1)); n < len(args) {
		args = args[:n]
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}
