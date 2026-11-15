package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(append([]string{"--config", dir}, args...), "test", &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestHelp(t *testing.T) {
	lang = "en"
	for _, args := range [][]string{{}, {"--help"}, {"-h"}, {"help"}} {
		code, out, _ := run(t, t.TempDir(), args...)
		if code != 0 || !strings.Contains(out, "Commands:") || !strings.Contains(out, "tfr") {
			t.Errorf("%v: code %d, out %q", args, code, out)
		}
	}
	code, out, _ := run(t, t.TempDir(), "help", "-tfr")
	if code != 0 || !strings.HasPrefix(out, "Usage: fileswap tfr") {
		t.Errorf("help -tfr: code %d, out %q", code, out)
	}
	code, out, _ = run(t, t.TempDir(), "init", "--help")
	if code != 0 || !strings.HasPrefix(out, "Usage: fileswap init") {
		t.Errorf("init --help: code %d, out %q", code, out)
	}
}

func TestSpanishHelp(t *testing.T) {
	lang = "es"
	defer func() { lang = "en" }()
	_, out, _ := run(t, t.TempDir(), "--help")
	if !strings.Contains(out, "Comandos:") {
		t.Errorf("spanish help missing: %q", out)
	}
}

func TestUnknownCommand(t *testing.T) {
	lang = "en"
	code, _, errOut := run(t, t.TempDir(), "frobnicate")
	if code != exitUsage || !strings.Contains(errOut, "unknown command") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestInitIDAllow(t *testing.T) {
	lang = "en"
	dir := t.TempDir()

	if code, _, errOut := run(t, dir, "id"); code != exitError || !strings.Contains(errOut, "fileswap init") {
		t.Fatalf("id before init: code %d, stderr %q", code, errOut)
	}
	if code, _, _ := run(t, dir, "init"); code != exitUsage {
		t.Fatalf("init without name: code %d, want %d", code, exitUsage)
	}

	code, out, errOut := run(t, dir, "init", "--name", "srv-01", "--server", "192.168.1.10:7341")
	if code != 0 {
		t.Fatalf("init: code %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "ws://192.168.1.10:7341/ws") || !strings.Contains(out, "http://192.168.1.10:7341/?pair=") {
		t.Fatalf("init output: %q", out)
	}
	code1 := codeLine(t, out)

	// Re-running init keeps the code and updates the name.
	_, out, _ = run(t, dir, "init", "--name", "srv-02")
	if codeLine(t, out) != code1 || !strings.Contains(out, "srv-02") {
		t.Fatalf("re-init changed the code or kept the name: %q", out)
	}
	// --force makes a new code.
	_, out, _ = run(t, dir, "init", "--force")
	if codeLine(t, out) == code1 {
		t.Fatal("init --force kept the same code")
	}

	if code, _, _ := run(t, dir, "allow", "nope"); code != exitError {
		t.Fatalf("allow bad code: %d", code)
	}
	if code, out, _ := run(t, dir, "allow", "A8AE6E6E-E929-ABEA-3AFC-FC5258C8CCD6"); code != 0 || !strings.Contains(out, "accepted automatically") {
		t.Fatalf("allow: %d %q", code, out)
	}
	if _, out, _ := run(t, dir, "allow", "a8ae6e6e-e929-abea-3afc-fc5258c8ccd6"); !strings.Contains(out, "already allowed") {
		t.Fatalf("allow twice: %q", out)
	}
	if _, out, _ := run(t, dir, "peers"); !strings.Contains(out, "No paired devices") {
		t.Fatalf("peers: %q", out)
	}
	if code, out, _ := run(t, dir, "status"); code != exitNotRunning || !strings.Contains(out, "listen is not running") {
		t.Fatalf("status: %d %q", code, out)
	}
	if code, out, _ := run(t, dir, "stop"); code != 0 || !strings.Contains(out, "not running") {
		t.Fatalf("stop: %d %q", code, out)
	}
}

func TestSwapFolderAndTfrArgs(t *testing.T) {
	lang = "en"
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if code, out, errOut := run(t, dir, "init", "--name", "srv"); code != 0 || !strings.Contains(out, "Folder:") || !strings.Contains(out, "/FileSwap") {
		t.Fatalf("init default folder: %d %q %q", code, out, errOut)
	}
	inbox := t.TempDir() + "/inbox"
	if code, out, _ := run(t, dir, "init", "--dir", inbox); code != 0 || !strings.Contains(out, "Folder:  "+inbox) {
		t.Fatalf("init --dir: %d %q", code, out)
	}
	if code, _, _ := run(t, dir, "tfr", "only-one-arg"); code != exitUsage {
		t.Fatalf("tfr with one arg: %d", code)
	}
	if code, _, errOut := run(t, dir, "tfr", "laptop", t.TempDir()); code != exitError || !strings.Contains(errOut, "not a regular file") {
		t.Fatalf("tfr folder: %d %q", code, errOut)
	}
	f := t.TempDir() + "/a.txt"
	os.WriteFile(f, []byte("x"), 0o600)
	if code, _, errOut := run(t, dir, "-tfr", "laptop", f); code != exitError || !strings.Contains(errOut, "no paired device matches") {
		t.Fatalf("tfr unknown node: %d %q", code, errOut)
	}
}

func codeLine(t *testing.T, out string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "Code:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "Code:"))
		}
	}
	t.Fatalf("no Code: line in %q", out)
	return ""
}

func TestNormalizeServer(t *testing.T) {
	cases := map[string]string{
		"localhost:7341":               "ws://localhost:7341/ws",
		"http://192.168.68.101:7341":   "ws://192.168.68.101:7341/ws",
		"https://fileswap.example.com": "wss://fileswap.example.com/ws",
		"ws://host:7341/ws":            "ws://host:7341/ws",
		"wss://host/other?x=1":         "wss://host/ws",
	}
	for in, want := range cases {
		got, err := NormalizeServer(in)
		if err != nil || got != want {
			t.Errorf("NormalizeServer(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://host", "http://"} {
		if _, err := NormalizeServer(bad); err == nil {
			t.Errorf("NormalizeServer(%q) accepted", bad)
		}
	}
}

func TestPairLink(t *testing.T) {
	got := PairLink("wss://fs.example.com/ws", "a8ae6e6e-e929-abea-3afc-fc5258c8ccd6", "srv 01")
	want := "https://fs.example.com/?pair=a8ae6e6e-e929-abea-3afc-fc5258c8ccd6&pname=srv+01"
	if got != want {
		t.Fatalf("PairLink = %q, want %q", got, want)
	}
}
