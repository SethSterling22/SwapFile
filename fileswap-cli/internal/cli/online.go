package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
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
	s := newStyle(e)
	if st == nil {
		fmt.Fprintln(e.errOut, fmt.Sprintf(t("peers.unreachable"), id.Server))
		peers := make([]node.Peer, 0, len(id.Pairs))
		for _, p := range id.Pairs {
			peers = append(peers, node.Peer{ID: p.ID, Name: p.Name})
		}
		printPeerTable(e, s, peers, false)
		return nil
	}
	printPeerTable(e, s, st.Peers, true)
	return nil
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
	s := newStyle(e)
	resp, err := control(context.Background(), id, node.Request{Cmd: "status"})
	if err != nil || resp.Status == nil {
		fmt.Fprintf(e.out, "%s  %s\n\n", s.bold(id.Name), s.state(false, false))
		printCode(e, s, id.ID())
		fmt.Fprintln(e.out)
		return &errExit{code: exitNotRunning, msg: "  " + t("status.notRunning")}
	}
	printStatus(e, s, resp.Status, "")
	return nil
}

// printStatus is the block shown by `status` and `start`.
func printStatus(e *env, s style, st *node.Status, logPath string) {
	fmt.Fprintf(e.out, "%s  %s\n\n", s.bold(st.Name), s.state(st.Online, !st.Online))
	printCode(e, s, st.ID)
	fmt.Fprintln(e.out)
	rows := [][2]string{{t("ui.server"), st.Server}}
	if st.Inbox != "" {
		rows = append(rows, [2]string{t("ui.folder"), short(st.Inbox)})
	}
	if logPath != "" {
		rows = append(rows, [2]string{t("ui.log"), short(logPath)})
	}
	rows = append(rows, [2]string{t("ui.running"), fmt.Sprintf(t("ui.runningValue"), time.Since(st.Since).Round(time.Second), st.PID, st.Version)})
	printRows(e, rows)
	fmt.Fprintln(e.out)
	printPeerTable(e, s, st.Peers, true)
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
		fmt.Fprintln(e.out, fmt.Sprintf(t("stop.notRunning"), id.Name))
		return nil
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			break
		}
	}
	fmt.Fprintln(e.out, fmt.Sprintf(t("stop.done"), id.Name))
	return nil
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// reasonMsg translates a transfer failure reason from the node package
// ("CODE", "CODE: detail" or "PEER_ABORTED:<abort reason>").
func reasonMsg(r string) string {
	if detail, ok := strings.CutPrefix(r, node.CodePeerAborted+":"); ok {
		detail = strings.TrimSpace(detail)
		if _, known := messages["en"]["abort."+detail]; known {
			detail = t("abort." + detail)
		}
		return tf("nerr."+node.CodePeerAborted, detail)
	}
	code, detail, _ := strings.Cut(r, ": ")
	if _, ok := messages["en"]["nerr."+code]; ok {
		return tf("nerr."+code, detail)
	}
	return r
}

// nodeErr translates node error codes into the user's language.
func nodeErr(err error, id *identity.Identity) error {
	var ne *node.Error
	if !errors.As(err, &ne) {
		return err
	}
	detail := ne.Detail
	switch {
	case ne.Code == node.CodeOffline && detail == "":
		detail = id.Server
	case ne.Code == node.CodePeerAborted:
		if _, ok := messages["en"]["abort."+detail]; ok {
			detail = t("abort." + detail)
		} else if detail == "" {
			detail = t("abort.CANCELLED")
		}
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
