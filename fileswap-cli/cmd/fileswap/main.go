// Command fileswap is the FileSwap command-line node: it pairs with browsers
// and other FileSwap nodes and transfers files with them peer to peer.
package main

import (
	"os"

	"github.com/SethSterling22/SwapFile/fileswap-cli/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=..."
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version, os.Stdout, os.Stderr))
}
