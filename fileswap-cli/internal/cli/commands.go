package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/identity"
)

func runInit(e *env, args []string) error {
	fs := newFlags("init")
	name := fs.String("name", "", "")
	server := fs.String("server", "", "")
	force := fs.Bool("force", false, "")
	dir := fs.String("dir", "", "")
	kind := fs.String("kind", "", "")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &errUsage{cmd: "init", msg: t("err.args")}
	}
	*name = strings.TrimSpace(*name)
	if *kind != "" && !identity.ValidKind(*kind) {
		return &errUsage{cmd: "init", msg: fmt.Sprintf(t("err.badKind"), *kind, strings.Join(identity.Kinds, ", "))}
	}
	inbox := ""
	if *dir != "" {
		d, err := resolveDir(*dir)
		if err != nil {
			return err
		}
		inbox = d
	}

	var srv string
	if *server != "" {
		s, err := NormalizeServer(*server)
		if err != nil {
			return err
		}
		srv = s
	}

	id, err := identity.Load(e.dir)
	switch {
	case errors.Is(err, identity.ErrNotInitialized) || (err == nil && *force):
		if *name == "" && id != nil {
			*name = id.Name
		}
		if *name == "" {
			return &errUsage{cmd: "init", msg: t("err.nameRequired")}
		}
		if srv == "" {
			srv = DefaultServer
			if id != nil {
				srv = id.Server
			}
		}
		if id, err = identity.New(*name, srv); err != nil {
			return err
		}
		id.Dir = inbox
		id.Kind = *kind
		if err := id.Save(e.dir); err != nil {
			return err
		}
		fmt.Fprintln(e.out, t("init.created"))
	case err != nil:
		return err
	default:
		id, err = identity.Update(e.dir, func(id *identity.Identity) error {
			if *name != "" {
				id.Name = *name
			}
			if srv != "" {
				id.Server = srv
			}
			if inbox != "" {
				id.Dir = inbox
			}
			if *kind != "" {
				id.Kind = *kind
			}
			return nil
		})
		if err != nil {
			return err
		}
		fmt.Fprintln(e.out, t("init.updated"))
	}
	fmt.Fprintln(e.out)
	printID(e, id, false)
	fmt.Fprintln(e.out)
	fmt.Fprintln(e.out, t("init.phi"))
	return nil
}

func runID(e *env, args []string) error {
	fs := newFlags("id")
	showQR := fs.Bool("qr", false, "")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &errUsage{cmd: "id", msg: t("err.args")}
	}
	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	printID(e, id, true)
	if *showQR {
		link := PairLink(id.Server, id.ID(), id.Name)
		lines, err := qrLines(link)
		if err != nil {
			return err
		}
		fmt.Fprintln(e.out)
		fmt.Fprintln(e.out, "  "+t("id.qr"))
		fmt.Fprintln(e.out)
		for _, l := range lines {
			fmt.Fprintln(e.out, "  "+l)
		}
	}
	return nil
}

// printID shows the identity: name, the device code in a box, server,
// swap folder and the browser pairing link (and the config dir for `id`).
func printID(e *env, id *identity.Identity, withConfig bool) {
	s := newStyle(e)
	fmt.Fprintln(e.out, kindEmoji(id.KindOrDefault(), id.Name)+" "+s.bold(id.Name))
	fmt.Fprintln(e.out)
	printCode(e, s, id.ID())
	fmt.Fprintln(e.out)
	rows := [][2]string{{t("ui.server"), id.Server}}
	if dir, err := id.Inbox(); err == nil {
		rows = append(rows, [2]string{t("ui.folder"), short(dir)})
	}
	if link := PairLink(id.Server, id.ID(), id.Name); link != "" {
		rows = append(rows, [2]string{t("ui.link"), link})
	}
	if withConfig {
		rows = append(rows, [2]string{t("ui.config"), short(e.dir)})
	}
	printRows(e, rows)
}

// resolveDir makes a swap folder path absolute, expanding a leading ~.
func resolveDir(dir string) (string, error) {
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	return filepath.Abs(dir)
}

// ensureInbox creates the swap folder (mode 0700: received files can be private).
func ensureInbox(dir string) error {
	st, err := os.Stat(dir)
	switch {
	case err == nil && !st.IsDir():
		return fmt.Errorf(t("err.notDir"), dir)
	case err == nil:
		return nil
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

func runAllow(e *env, args []string) error {
	fs := newFlags("allow")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return &errUsage{cmd: "allow", msg: t("err.args")}
	}
	code := strings.ToLower(strings.TrimSpace(fs.Arg(0)))
	if !identity.IsDeviceID(code) {
		return fmt.Errorf(t("err.badCode"), fs.Arg(0))
	}
	if _, err := loadIdentity(e); err != nil {
		return err
	}
	exists := false
	if _, err := identity.Update(e.dir, func(id *identity.Identity) error {
		if exists = id.Allowed(code); !exists {
			id.Allow = append(id.Allow, code)
		}
		return nil
	}); err != nil {
		return err
	}
	if exists {
		fmt.Fprintln(e.out, fmt.Sprintf(t("allow.exists"), code))
	} else {
		fmt.Fprintln(e.out, fmt.Sprintf(t("allow.added"), code))
	}
	return nil
}

func runVersion(e *env, args []string) error {
	fmt.Fprintln(e.out, "fileswap", e.version)
	return nil
}

func runHelp(e *env, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(e.out, t("usage"))
		return nil
	}
	name := args[0]
	if a, ok := aliases[name]; ok {
		name = a
	}
	if _, ok := messages["en"]["help."+name]; !ok {
		return &errUsage{cmd: "help", msg: fmt.Sprintf(t("err.unknownCommand"), args[0])}
	}
	fmt.Fprint(e.out, t("help."+name))
	return nil
}
