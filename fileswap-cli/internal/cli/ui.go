package cli

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/node"
)

// Output blocks shared by start, status, peers and id: a title line, the
// device code in a box, aligned "label  value" rows and the peers table.
// Colors and bold are only used on a terminal (not with NO_COLOR or
// TERM=dumb), so logs and pipes stay plain text.

type style struct{ on bool }

func newStyle(e *env) style {
	return style{on: e.out == os.Stdout && isTerminal(os.Stdout) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"}
}

func (s style) wrap(code, text string) string {
	if !s.on {
		return text
	}
	return "\033[" + code + "m" + text + "\033[0m"
}

func (s style) bold(t string) string  { return s.wrap("1", t) }
func (s style) dim(t string) string   { return s.wrap("2", t) }
func (s style) green(t string) string { return s.wrap("32", t) }

// kindEmoji is a device's icon: its announced kind, else a guess from its
// name (devices on older versions announce none). Same icons as the web app.
func kindEmoji(kind, name string) string {
	switch kind {
	case "server":
		return "🌐"
	case "desktop":
		return "🖥️"
	case "laptop":
		return "💻"
	case "phone":
		return "📱"
	case "tablet":
		return "📲"
	}
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "server") || strings.Contains(n, "servidor") || strings.Contains(n, "srv") || strings.Contains(n, "nas"):
		return "🌐"
	case strings.Contains(n, "ipad") || strings.Contains(n, "tablet"):
		return "📲"
	case strings.Contains(n, "desktop") || strings.Contains(n, "escritorio") || strings.Contains(n, "imac"):
		return "🖥️"
	case strings.Contains(n, "mac") || strings.Contains(n, "pc") || strings.Contains(n, "laptop") || strings.Contains(n, "portátil"):
		return "💻"
	}
	return "📱"
}

// width is how many terminal columns s takes: emoji take two, the emoji
// variation selector none.
func width(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r == 0xFE0F:
		case r >= 0x1F300 && r <= 0x1FAFF:
			w += 2
		default:
			w++
		}
	}
	return w
}

// state renders ● online / ○ offline / ◌ connecting.
func (s style) state(online bool, connecting bool) string {
	switch {
	case online:
		return s.green("● " + t("ui.online"))
	case connecting:
		return "◌ " + t("ui.connecting")
	default:
		return s.dim("○ " + t("ui.offline"))
	}
}

// printCode prints the device code, bold, in a box under its label.
func printCode(e *env, s style, code string) {
	bar := strings.Repeat("─", utf8.RuneCountInString(code)+2)
	fmt.Fprintln(e.out, "  "+t("ui.code"))
	fmt.Fprintln(e.out, "  ╭"+bar+"╮")
	fmt.Fprintln(e.out, "  │ "+s.bold(code)+" │")
	fmt.Fprintln(e.out, "  ╰"+bar+"╯")
}

// printRows prints label/value pairs with the values aligned.
func printRows(e *env, rows [][2]string) {
	w := 0
	for _, r := range rows {
		w = max(w, utf8.RuneCountInString(r[0]))
	}
	for _, r := range rows {
		fmt.Fprintf(e.out, "  %s%s  %s\n", r[0], strings.Repeat(" ", w-utf8.RuneCountInString(r[0])), r[1])
	}
}

// short shows a path under the home directory as ~/…
func short(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+"/"); ok {
		return "~/" + rest
	}
	return path
}

// printPeerTable prints the paired devices as a table, online ones first.
func printPeerTable(e *env, s style, peers []node.Peer, known bool) {
	fmt.Fprintf(e.out, "  %s (%d)\n", t("ui.paired"), len(peers))
	if len(peers) == 0 {
		fmt.Fprintln(e.out, "  "+s.dim(t("peers.none")))
		return
	}
	stateW, nameW := width(t("ui.col.status")), width(t("ui.col.name"))
	states := make([]string, len(peers))
	names := make([]string, len(peers))
	for i, p := range peers {
		plain := "? " + t("ui.unknown")
		if known {
			plain = map[bool]string{true: "● " + t("ui.online"), false: "○ " + t("ui.offline")}[p.Online]
		}
		states[i] = plain
		names[i] = kindEmoji(p.Kind, p.Name) + " " + p.Name
		stateW = max(stateW, width(plain))
		nameW = max(nameW, width(names[i]))
	}
	pad := func(text string, w int) string { return text + strings.Repeat(" ", max(0, w-width(text))) }
	fmt.Fprintf(e.out, "    %s  %s  %s\n", s.dim(pad(t("ui.col.status"), stateW)), s.dim(pad(t("ui.col.name"), nameW)), s.dim(t("ui.col.code")))
	for i, p := range peers {
		st := pad(states[i], stateW)
		switch {
		case !known:
		case p.Online:
			st = s.green(st)
		default:
			st = s.dim(st)
		}
		fmt.Fprintf(e.out, "    %s  %s  %s\n", st, pad(names[i], nameW), p.ID)
	}
}
