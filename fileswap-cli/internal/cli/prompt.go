package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// prompter asks y/N questions on the terminal, one at a time.
//
// `fileswap listen &` must keep running: a background job that reads the
// terminal is stopped by the shell (SIGTTIN). So SIGTTIN/SIGTTOU are
// ignored — reads then fail with EIO while in the background and are
// retried — and a question is only asked while listen is the foreground
// job; otherwise the request is rejected with a hint.
type prompter struct {
	e     *env
	mu    sync.Mutex
	lines chan string
}

func newPrompter(e *env) *prompter {
	signal.Ignore(syscall.SIGTTIN, syscall.SIGTTOU)
	p := &prompter{e: e, lines: make(chan string)}
	go p.read()
	return p
}

func (p *prompter) read() {
	rd := bufio.NewReader(os.Stdin)
	for {
		line, err := rd.ReadString('\n')
		if err == nil {
			// Only deliver an answer to a question being asked right now, so
			// an Enter pressed earlier never answers a later request.
			select {
			case p.lines <- line:
			default:
			}
			continue
		}
		if errors.Is(err, io.EOF) {
			close(p.lines)
			return
		}
		time.Sleep(500 * time.Millisecond) // EIO while in the background
	}
}

func (p *prompter) decide(ctx context.Context, code, name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !isForeground(os.Stdin) {
		fmt.Fprintln(p.e.out, "  "+t("prompt.background"))
		return false
	}
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

// isForeground reports whether this process is the terminal's foreground job.
func isForeground(f *os.File) bool {
	var pgrp int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCGPGRP), uintptr(unsafe.Pointer(&pgrp)))
	return errno == 0 && int(pgrp) == syscall.Getpgrp()
}
