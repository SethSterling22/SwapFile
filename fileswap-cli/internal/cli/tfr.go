package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/node"
)

func runTfr(e *env, args []string) error {
	fs := newFlags("tfr")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return &errUsage{cmd: "tfr", msg: t("err.args")}
	}
	ref := fs.Arg(0)
	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	var paths []string
	for _, p := range fs.Args()[1:] {
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		st, err := os.Stat(abs)
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() && !st.IsDir() {
			return nodeErr(&node.Error{Code: node.CodeNotFile, Detail: p}, id)
		}
		paths = append(paths, abs)
	}
	if p, err := id.Find(ref); err != nil {
		return nodeErr(&node.Error{Code: node.CodeAmbiguous, Detail: ref}, id)
	} else if p == nil {
		return nodeErr(&node.Error{Code: node.CodeNotFound, Detail: ref}, id)
	}

	// Ctrl+C cancels the transfer and tells the other device.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	label := filepath.Base(paths[0])
	if len(paths) > 1 {
		label = fmt.Sprintf(t("tfr.many"), len(paths))
	}
	bar := newProgressBar(e, label)
	defer bar.finish()

	var res *node.SendResult
	if resp, cerr := node.CallStream(ctx, node.SocketPath(id.ID()), node.Request{Cmd: "send", Arg: ref, Paths: paths},
		func(p node.Progress) { bar.update(p.Done, p.Total) }); cerr == nil {
		res, err = resp.Sent, resp.AsError()
	} else if ctx.Err() != nil {
		return nodeErr(&node.Error{Code: "CANCELLED"}, id)
	} else if !errors.Is(cerr, node.ErrNotRunning) {
		return cerr // listen is running but the control connection broke
	} else {
		err = oneShot(e, func(_ context.Context, n *node.Node, online bool) error {
			if !online {
				return &node.Error{Code: node.CodeOffline}
			}
			var serr error
			res, serr = n.Send(ctx, ref, paths, bar.update)
			return serr
		})
	}
	bar.finish()
	if ctx.Err() != nil {
		return nodeErr(&node.Error{Code: "CANCELLED"}, id)
	}
	if err != nil {
		return nodeErr(err, id)
	}
	if res.Items > 0 {
		fmt.Fprintln(e.out, fmt.Sprintf(t("tfr.doneZip"), res.Items, res.Name, fmtSize(res.Size), res.Peer.Name, res.SHA256[:16]))
	} else {
		fmt.Fprintln(e.out, fmt.Sprintf(t("tfr.done"), res.Name, fmtSize(res.Size), res.Peer.Name, res.SHA256[:16]))
	}
	return nil
}

// progressBar draws a one-line progress bar on stderr when it is a terminal.
type progressBar struct {
	e     *env
	name  string
	tty   bool
	start time.Time
	last  time.Time
	shown bool
}

func newProgressBar(e *env, name string) *progressBar {
	return &progressBar{e: e, name: name, tty: e.errOut == os.Stderr && isTerminal(os.Stderr), start: time.Now()}
}

func (b *progressBar) update(done, total int64) {
	if !b.tty || (time.Since(b.last) < 100*time.Millisecond && done != total) {
		return
	}
	b.last = time.Now()
	pct := 100.0
	if total > 0 {
		pct = float64(done) * 100 / float64(total)
	}
	const width = 24
	filled := int(pct / 100 * width)
	rate := float64(done) / max(time.Since(b.start).Seconds(), 0.001)
	name := b.name
	if len([]rune(name)) > 24 {
		name = string([]rune(name)[:23]) + "…"
	}
	fmt.Fprintf(b.e.errOut, "\r\033[K%s [%s%s] %3.0f%%  %s / %s  %s/s",
		name, strings.Repeat("█", filled), strings.Repeat("░", width-filled), pct, fmtSize(done), fmtSize(total), fmtSize(int64(rate)))
	b.shown = true
}

func (b *progressBar) finish() {
	if b.shown {
		fmt.Fprint(b.e.errOut, "\r\033[K")
		b.shown = false
	}
}

func fmtSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
