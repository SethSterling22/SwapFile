package cli

import (
	"fmt"
	"net/url"
	"strings"
)

// DefaultServer is used by `init` when --server is not given.
const DefaultServer = "ws://localhost:7341/ws"

// NormalizeServer accepts the forms people actually type — "host:7341",
// "http://host:7341", "https://example.com", "ws://host:7341/ws" — and
// returns the WebSocket signaling URL (ws:// or wss://, path /ws).
func NormalizeServer(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf(t("err.badServer"), raw)
	}
	if !strings.Contains(s, "://") {
		s = "ws://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf(t("err.badServer"), raw)
	}
	switch u.Scheme {
	case "ws", "http":
		u.Scheme = "ws"
	case "wss", "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf(t("err.badServer"), raw)
	}
	u.Path = "/ws"
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return u.String(), nil
}

// PairLink is the web link that opens the app and asks to pair with this node,
// the same one the browser client puts in its QR code.
func PairLink(server, deviceID, name string) string {
	u, err := url.Parse(server)
	if err != nil {
		return ""
	}
	if u.Scheme == "wss" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	u.Path = "/"
	q := url.Values{}
	q.Set("pair", deviceID)
	q.Set("pname", name)
	u.RawQuery = q.Encode()
	return u.String()
}
