package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/clip"
	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/identity"
	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/node"
)

// `fileswap clip`: this node's clipboard (texts and attachments that paired
// devices can see and take while the node is online) and the clipboards of
// the other devices.

func runClip(e *env, args []string) error {
	if len(args) == 0 {
		return &errUsage{cmd: "clip", msg: t("err.args")}
	}
	switch args[0] {
	case "add":
		return clipAdd(e, args[1:])
	case "ls", "list":
		return clipLs(e, args[1:])
	case "get":
		return clipGet(e, args[1:])
	case "rm", "remove":
		return clipRm(e, args[1:])
	default:
		return &errUsage{cmd: "clip", msg: t("err.args")}
	}
}

func ownStore(e *env) (*identity.Identity, *clip.Store, error) {
	id, err := loadIdentity(e)
	if err != nil {
		return nil, nil, err
	}
	inbox, err := id.Inbox()
	if err != nil {
		return nil, nil, err
	}
	return id, clip.New(inbox), nil
}

func clipErr(err error) error {
	switch {
	case errors.Is(err, clip.ErrEmpty):
		return errors.New(t("clip.errEmpty"))
	case errors.Is(err, clip.ErrTooLong):
		return fmt.Errorf(t("clip.errTooLong"), clip.MaxText>>10)
	case errors.Is(err, clip.ErrFull):
		return fmt.Errorf(t("clip.errFull"), fmtSize(clip.DefaultLimit), clip.MaxItems)
	case errors.Is(err, clip.ErrNotFound):
		return errors.New(t("clip.errNotFound"))
	}
	return err
}

func clipAdd(e *env, args []string) error {
	fs := newFlags("clip")
	text := fs.String("t", "", "")
	fs.StringVar(text, "text", "", "")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return &errUsage{cmd: "clip", msg: t("err.args")}
	}
	// `-t -`, or nothing at all with a pipe on stdin: the text comes from stdin
	if *text == "-" || (*text == "" && fs.NArg() == 0 && !isTerminal(os.Stdin)) {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, clip.MaxText+1))
		if err != nil {
			return err
		}
		*text = string(b)
	}
	file := ""
	if fs.NArg() == 1 {
		abs, err := filepath.Abs(fs.Arg(0))
		if err != nil {
			return err
		}
		file = abs
	}
	id, store, err := ownStore(e)
	if err != nil {
		return err
	}
	item, err := store.Add(*text, file)
	if err != nil {
		return clipErr(err)
	}
	s := newStyle(e)
	fmt.Fprintln(e.out, fmt.Sprintf(t("clip.added"), s.bold(item.ID), fmtLeft(time.Until(item.Expires))))
	printClipContent(e, s, item.Text, item.File, item.Size, "  ")
	if _, err := control(context.Background(), id, node.Request{Cmd: "status"}); err != nil {
		fmt.Fprintln(e.out)
		fmt.Fprintln(e.out, "  "+s.dim(t("clip.notOnline")))
	}
	return nil
}

func clipRm(e *env, args []string) error {
	if len(args) != 1 {
		return &errUsage{cmd: "clip", msg: t("err.args")}
	}
	_, store, err := ownStore(e)
	if err != nil {
		return err
	}
	n, err := store.Remove(args[0])
	if err != nil {
		return clipErr(err)
	}
	fmt.Fprintln(e.out, fmt.Sprintf(t("clip.removed"), n))
	return nil
}

func clipLs(e *env, args []string) error {
	if len(args) > 1 {
		return &errUsage{cmd: "clip", msg: t("err.args")}
	}
	s := newStyle(e)
	if len(args) == 0 {
		_, store, err := ownStore(e)
		if err != nil {
			return err
		}
		list, err := node.ListClipboard(store)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.out, "%s  %s\n\n", s.bold(t("clip.mine")), s.dim(fmt.Sprintf(t("clip.usage"), len(list.Items), fmtSize(list.Used), fmtSize(list.Limit))))
		printClipTable(e, s, list.Items, true)
		return nil
	}

	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	list, peerID, err := remoteClipboard(e, id, args[0])
	name := args[0]
	if p, _ := id.Find(args[0]); p != nil {
		name, peerID = p.Name, p.ID
	}
	if err == nil {
		saveClipCache(peerID, list)
		fmt.Fprintf(e.out, "%s  %s  %s\n\n", s.bold(fmt.Sprintf(t("clip.theirs"), name)), s.state(true, false), s.dim(fmt.Sprintf(t("clip.usage"), len(list.Items), fmtSize(list.Used), fmtSize(list.Limit))))
		printClipTable(e, s, list.Items, true)
		return nil
	}
	cached, at, cerr := loadClipCache(peerID)
	var ne *node.Error
	if cerr != nil || !errors.As(err, &ne) || (ne.Code != "DEVICE_OFFLINE" && ne.Code != node.CodeOffline && ne.Code != node.CodeConnectTimeout) {
		return nodeErr(err, id)
	}
	// Offline: the last list seen, without the items that have expired since
	fmt.Fprintf(e.out, "%s  %s  %s\n\n", s.bold(fmt.Sprintf(t("clip.theirs"), name)), s.state(false, false), s.dim(fmt.Sprintf(t("clip.seen"), fmtAgo(time.Since(at)))))
	printClipTable(e, s, cached.Items, false)
	fmt.Fprintln(e.out)
	fmt.Fprintln(e.out, "  "+s.dim(t("clip.offlineNote")))
	return nil
}

func clipGet(e *env, args []string) error {
	fs := newFlags("clip")
	dirFlag := fs.String("dir", "", "")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return &errUsage{cmd: "clip", msg: t("err.args")}
	}
	ref, itemRef := fs.Arg(0), strings.ToLower(fs.Arg(1))
	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	list, _, err := remoteClipboard(e, id, ref)
	if err != nil {
		return nodeErr(err, id)
	}
	var item *node.RemoteItem
	for i, it := range list.Items {
		if strings.HasPrefix(it.ID, itemRef) {
			if item != nil {
				return errors.New(t("clip.errAmbiguous"))
			}
			item = &list.Items[i]
		}
	}
	if item == nil {
		return errors.New(t("clip.errNotFound"))
	}
	// The text goes to stdout as it is, so it can be piped or copied
	if item.Text != "" {
		fmt.Fprintln(e.out, item.Text)
	}
	if item.File == nil {
		return nil
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
	fmt.Fprintln(e.errOut, fmt.Sprintf(t("clip.getting"), item.File.Name, fmtSize(item.File.Size)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var saved *node.Received
	if resp, cerr := control(ctx, id, node.Request{Cmd: "clipget", Arg: ref, Item: item.ID}); cerr == nil {
		saved, err = resp.Received, resp.AsError()
		if *dirFlag != "" && err == nil {
			fmt.Fprintln(e.errOut, t("clip.dirIgnored"))
		}
	} else if errors.Is(cerr, node.ErrNotRunning) {
		err = oneShotWith(e, node.Options{InboxDir: inbox}, func(ctx context.Context, n *node.Node, online bool) error {
			if !online {
				return &node.Error{Code: node.CodeOffline}
			}
			res, gerr := n.ClipGet(ctx, ref, item.ID)
			if res != nil {
				saved = &node.Received{Path: res.Path, Size: res.Meta.Size, SHA256: res.SHA256}
			}
			return gerr
		})
	} else {
		return cerr
	}
	if err != nil {
		return nodeErr(err, id)
	}
	fmt.Fprintln(e.errOut, fmt.Sprintf(t("clip.saved"), short(saved.Path), fmtSize(saved.Size), saved.SHA256[:16]))
	return nil
}

// remoteClipboard asks the running node, or connects just for this.
func remoteClipboard(e *env, id *identity.Identity, ref string) (*node.ClipList, string, error) {
	ctx := context.Background()
	if resp, err := control(ctx, id, node.Request{Cmd: "clipls", Arg: ref}); err == nil {
		return resp.Clip, resp.PeerID, resp.AsError()
	} else if !errors.Is(err, node.ErrNotRunning) {
		return nil, "", err
	}
	var list *node.ClipList
	var peerID string
	err := oneShot(e, func(ctx context.Context, n *node.Node, online bool) error {
		if !online {
			return &node.Error{Code: node.CodeOffline}
		}
		var lerr error
		list, peerID, lerr = n.ClipboardOf(ctx, ref)
		return lerr
	})
	return list, peerID, err
}

func printClipTable(e *env, s style, items []node.RemoteItem, online bool) {
	now := time.Now()
	shown := 0
	idW, expW := utf8.RuneCountInString(t("clip.colID")), utf8.RuneCountInString(t("clip.colExpires"))
	for _, it := range items {
		expW = max(expW, utf8.RuneCountInString(fmtLeft(time.UnixMilli(it.Expires).Sub(now))))
		idW = max(idW, len(it.ID))
	}
	pad := func(text string, w int) string {
		return text + strings.Repeat(" ", max(0, w-utf8.RuneCountInString(text)))
	}
	for _, it := range items {
		left := time.UnixMilli(it.Expires).Sub(now)
		if left <= 0 {
			continue
		}
		if shown == 0 {
			fmt.Fprintf(e.out, "    %s  %s  %s\n", s.dim(pad(t("clip.colID"), idW)), s.dim(pad(t("clip.colExpires"), expW)), s.dim(t("clip.colContent")))
		}
		shown++
		id := pad(it.ID, idW)
		if !online {
			id = s.dim(id)
		}
		var size int64
		file := ""
		if it.File != nil {
			file, size = it.File.Name, it.File.Size
		}
		fmt.Fprintf(e.out, "    %s  %s  ", id, pad(fmtLeft(left), expW))
		printClipContent(e, s, it.Text, file, size, strings.Repeat(" ", 4+idW+2+expW+2))
	}
	if shown == 0 {
		fmt.Fprintln(e.out, "    "+s.dim(t("clip.empty")))
	}
}

// printClipContent prints the text (first line, shortened) and the attachment.
func printClipContent(e *env, s style, text, file string, size int64, indent string) {
	lines := 0
	if text != "" {
		first, rest, _ := strings.Cut(text, "\n")
		if utf8.RuneCountInString(first) > 60 {
			first = string([]rune(first)[:59]) + "…"
		} else if rest != "" {
			first += " …"
		}
		if indent == "  " { // `clip add` output starts its own line
			fmt.Fprint(e.out, indent)
		}
		fmt.Fprintln(e.out, "📝 "+first)
		lines++
	}
	if file != "" {
		if lines > 0 || indent == "  " {
			fmt.Fprint(e.out, indent)
		}
		fmt.Fprintln(e.out, fmt.Sprintf("📎 %s (%s)", file, fmtSize(size)))
	}
}

// fmtLeft is a remaining time: "23 h", "45 min".
func fmtLeft(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%d h", int(d.Hours()+0.5))
	}
	return fmt.Sprintf("%d min", max(1, int(d.Minutes()+0.5)))
}

// fmtAgo is an elapsed time: "5 min", "2 h", "3 d".
func fmtAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return t("clip.justNow")
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
	return fmt.Sprintf("%d d", int(d.Hours()/24))
}

// ─── Last seen clipboards of other devices ────────────────────────────────

type clipCache struct {
	Fetched int64          `json:"fetched"`
	List    *node.ClipList `json:"list"`
}

func clipCachePath(peerID string) (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	if !identity.IsDeviceID(peerID) {
		return "", errors.New("bad device code")
	}
	return filepath.Join(dir, "fileswap", "clipcache", peerID+".json"), nil
}

func saveClipCache(peerID string, list *node.ClipList) {
	path, err := clipCachePath(peerID)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	b, _ := json.Marshal(clipCache{Fetched: time.Now().UnixMilli(), List: list})
	_ = os.WriteFile(path, b, 0o600)
}

func loadClipCache(peerID string) (*node.ClipList, time.Time, error) {
	path, err := clipCachePath(peerID)
	if err != nil {
		return nil, time.Time{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var c clipCache
	if err := json.Unmarshal(b, &c); err != nil || c.List == nil {
		return nil, time.Time{}, errors.New("bad cache")
	}
	return c.List, time.UnixMilli(c.Fetched), nil
}
