package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/identity"
)

func runInit(e *env, args []string) error {
	fs := newFlags("init")
	name := fs.String("name", "", "")
	server := fs.String("server", "", "")
	force := fs.Bool("force", false, "")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &errUsage{cmd: "init", msg: t("err.args")}
	}
	*name = strings.TrimSpace(*name)

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
		if err := id.Save(e.dir); err != nil {
			return err
		}
		fmt.Fprintln(e.out, t("init.created"))
	case err != nil:
		return err
	default:
		if *name != "" {
			id.Name = *name
		}
		if srv != "" {
			id.Server = srv
		}
		if err := id.Save(e.dir); err != nil {
			return err
		}
		fmt.Fprintln(e.out, t("init.updated"))
	}
	fmt.Fprintln(e.out)
	printID(e, id)
	fmt.Fprintln(e.out)
	fmt.Fprintln(e.out, t("init.phi"))
	return nil
}

func runID(e *env, args []string) error {
	fs := newFlags("id")
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
	printID(e, id)
	fmt.Fprintln(e.out, fmt.Sprintf(t("id.config"), e.dir))
	return nil
}

func printID(e *env, id *identity.Identity) {
	fmt.Fprintln(e.out, fmt.Sprintf(t("id.name"), id.Name))
	fmt.Fprintln(e.out, fmt.Sprintf(t("id.code"), id.ID()))
	fmt.Fprintln(e.out, fmt.Sprintf(t("id.server"), id.Server))
	if link := PairLink(id.Server, id.ID(), id.Name); link != "" {
		fmt.Fprintln(e.out, fmt.Sprintf(t("id.link"), link))
	}
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
	for _, p := range id.Pairs {
		fmt.Fprintf(e.out, "%s  %s\n", p.ID, p.Name)
	}
	return nil
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
	id, err := loadIdentity(e)
	if err != nil {
		return err
	}
	for _, a := range id.Allow {
		if a == code {
			fmt.Fprintln(e.out, fmt.Sprintf(t("allow.exists"), code))
			return nil
		}
	}
	id.Allow = append(id.Allow, code)
	if err := id.Save(e.dir); err != nil {
		return err
	}
	fmt.Fprintln(e.out, fmt.Sprintf(t("allow.added"), code))
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
