// Package cli implements the fileswap command line: argument parsing,
// help, and the commands themselves.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/identity"
)

// Exit codes.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// aliases maps the flag-style spellings to their command.
var aliases = map[string]string{
	"-sync": "sync", "--sync": "sync",
	"-tfr": "tfr", "--tfr": "tfr", "send": "tfr",
	"-h": "help", "--help": "help", "-help": "help",
	"-v": "version", "--version": "version",
}

type env struct {
	dir     string
	version string
	out     io.Writer
	errOut  io.Writer
}

type command struct {
	run func(e *env, args []string) error
}

var commands = map[string]command{
	"init":    {runInit},
	"id":      {runID},
	"peers":   {runPeers},
	"allow":   {runAllow},
	"sync":    {runSync},
	"tfr":     {runTfr},
	"start":   {runStart},
	"listen":  {runListen},
	"stop":    {runStop},
	"service": {runService},
	"clip":    {runClip},
	"status":  {runStatus},
	"unpair":  {runUnpair},
	"version": {runVersion},
	"help":    {runHelp},
}

// errUsage marks argument errors; Run prints the command's help and exits 2.
type errUsage struct{ cmd, msg string }

func (e *errUsage) Error() string { return e.msg }

// Run executes the command line and returns the process exit code.
func Run(args []string, version string, out, errOut io.Writer) int {
	code := runCommand(args, version, out, errOut)
	// On a terminal, leave a blank line after the output so the next
	// prompt does not stick to it. Pipes and logs get no extra line.
	if f, ok := out.(*os.File); ok && isTerminal(f) {
		fmt.Fprintln(out)
	}
	return code
}

func runCommand(args []string, version string, out, errOut io.Writer) int {
	e := &env{version: version, out: out, errOut: errOut}

	// Global options come before the command.
	for len(args) > 0 && strings.HasPrefix(args[0], "--config") {
		if v, ok := strings.CutPrefix(args[0], "--config="); ok {
			e.dir, args = v, args[1:]
		} else if len(args) > 1 {
			e.dir, args = args[1], args[2:]
		} else {
			fmt.Fprint(errOut, t("usage"))
			return exitUsage
		}
	}
	if len(args) == 0 {
		fmt.Fprint(out, t("usage"))
		return exitOK
	}

	name := args[0]
	if a, ok := aliases[name]; ok {
		name = a
	}
	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintln(errOut, "fileswap:", fmt.Sprintf(t("err.unknownCommand"), args[0]))
		return exitUsage
	}
	rest := args[1:]
	if name != "help" && len(rest) > 0 && isHelpFlag(rest[0]) {
		fmt.Fprint(out, t("help."+name))
		return exitOK
	}

	if e.dir == "" {
		d, err := identity.DefaultDir()
		if err != nil {
			fmt.Fprintln(errOut, "fileswap:", err)
			return exitError
		}
		e.dir = d
	}

	if err := cmd.run(e, rest); err != nil {
		var u *errUsage
		if errors.As(err, &u) {
			fmt.Fprintln(errOut, "fileswap:", u.msg)
			fmt.Fprint(errOut, "\n"+t("help."+u.cmd))
			return exitUsage
		}
		var x *errExit
		if errors.As(err, &x) {
			fmt.Fprintln(out, x.msg)
			return x.code
		}
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(out, t("help."+name))
			return exitOK
		}
		fmt.Fprintln(errOut, "fileswap:", err)
		return exitError
	}
	return exitOK
}

func isHelpFlag(s string) bool { return s == "-h" || s == "--help" || s == "-help" }

// newFlags returns a quiet FlagSet: errors are reported by Run with the
// translated command help instead of the flag package's English usage.
func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return &errUsage{cmd: fs.Name(), msg: err.Error()}
	}
	return nil
}

func loadIdentity(e *env) (*identity.Identity, error) {
	id, err := identity.Load(e.dir)
	if errors.Is(err, identity.ErrNotInitialized) {
		return nil, errors.New(t("err.notInit"))
	}
	return id, err
}
