package cli

import (
	"strings"

	"rsc.io/qr"
)

const qrMargin = 2 // quiet zone, in modules

// qrLines renders text as a QR code with half-block characters, two module
// rows per line. Like `qrencode -t UTF8`, the blocks are the light modules,
// so the code has the right polarity on the usual dark terminal.
func qrLines(text string) ([]string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, err
	}
	light := func(x, y int) bool { return !code.Black(x-qrMargin, y-qrMargin) }
	n := code.Size + 2*qrMargin
	var lines []string
	for y := 0; y < n; y += 2 {
		var b strings.Builder
		for x := 0; x < n; x++ {
			top, bottom := light(x, y), y+1 >= n || light(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		lines = append(lines, b.String())
	}
	return lines, nil
}
