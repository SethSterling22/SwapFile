package cli

import (
	"fmt"
	"os"
	"strings"
)

// Messages follow the system locale like the web client follows the browser:
// any es_* locale gets Spanish, everything else English.
func detectLang() string {
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if l := os.Getenv(v); l != "" {
			if strings.HasPrefix(strings.ToLower(l), "es") {
				return "es"
			}
			return "en"
		}
	}
	return "en"
}

var lang = detectLang()

func t(key string, args ...any) string {
	s, ok := messages[lang][key]
	if !ok {
		s = messages["en"][key]
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

var messages = map[string]map[string]string{
	"en": {
		"usage": `fileswap — send files between browsers and Linux servers, peer to peer

Usage:
  fileswap [--config <dir>] <command> [options]

Commands:
  init     --name <name> [--server <url>] [--dir <path>]
                                             Create this node's identity
  id       [--qr]                            Show this node's name, code and pairing link (--qr: as a QR code)
  sync     <code>                            Pair with a device (it must accept)    (alias: -sync)
  peers                                      List paired devices and who is online
  unpair   <node>                            Remove a paired device on both sides
  start    [-f] [--dir <path>]               Go online in the background to receive files (-f: foreground)
  status                                     Show whether it is online, and the paired devices
  stop                                       Go offline
  service  install|remove                    Run it as a systemd user service (starts at login/boot)
  allow    <code>                            Accept pairing from this device without asking
  tfr      <node> <path>...                  Send files or folders to a paired device (several → one ZIP) (alias: -tfr, send)
  version                                    Print the version
  help     [command]                         Show this help, or a command's help    (alias: --help, -h)

<node> is a device code, a unique prefix of it, or the device's name.
<code> is the full device code (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
` + "`fileswap listen`" + ` is ` + "`fileswap start -f`" + `.

Received files go to the swap folder: --dir, else the one set with
` + "`init --dir`" + `, else ~/FileSwap.

fileswap never opens a port: it only connects out to the FileSwap server and,
for transfers, straight to the other device.

Options:
  --config <dir>   Config directory (default: $FILESWAP_CONFIG, else $XDG_CONFIG_HOME/fileswap, else ~/.config/fileswap)
`,
		"help.init": `Usage: fileswap init --name <name> [--server <url>] [--dir <path>] [--force]

Creates this node's identity: a private secret stored in the config directory
and the public device code derived from it. Running init again keeps the code
and only updates the name or server; --force creates a new code (paired devices
will have to pair again).

  --name     Device name shown to other devices (required the first time)
  --server   FileSwap server, e.g. ws://192.168.1.10:7341 or https://fileswap.example.com
             (default: ws://localhost:7341)
  --dir      Swap folder where received files are saved (default: ~/FileSwap)
  --force    Replace the identity with a new one
`,
		"help.id":    "Usage: fileswap id [--qr]\n\nShows this node's name, device code, server, swap folder and the link that\npairs a browser with it.\n\n  --qr   Also draw the pairing link as a QR code: scan it with a phone to open\n         FileSwap and pair (the node must be online: fileswap start).\n",
		"help.peers": "Usage: fileswap peers\n\nLists the paired devices and whether each one is online now.\n",
		"help.sync": `Usage: fileswap sync <code>

Sends a pairing request to the device with that code and waits (up to 2 min)
until it is accepted there. The other device must be online: a browser with
FileSwap open, or a node running ` + "`fileswap start`" + `.
`,
		"help.tfr": `Usage: fileswap tfr <node> <path>...

Sends files straight to a paired device (peer to peer, WebRTC) and waits
until the other side confirms it received every byte (SHA-256 checked).

One file is sent as it is. Several files, or folders, are packed into a
single ZIP while sending — on this machine, never on the server — and
arrive as one .zip ("<folder>.zip" for one folder). Photos, videos and
other compressed formats are stored as they are; the rest is deflated.

The other device must be online: a browser with FileSwap open, or a node
running ` + "`fileswap start`" + `. Ctrl+C cancels the transfer on both sides.

<node> is a device code, a unique prefix of it, or the device's name.
`,
		"help.start": `Usage: fileswap start [-f] [--dir <path>]

Puts this node online in the background and returns the terminal: paired
browsers and nodes can send it files, which are saved in the swap folder
(never overwriting). Events go to ~/.local/state/fileswap/listen.log.
No port is opened: the node only connects out to the FileSwap server.

Pairing in the background only accepts devices already paired or in the
allow list. To pair a new one, either run ` + "`fileswap sync <code>`" + ` and accept
it in the browser, or ` + "`fileswap allow <code>`" + ` first.

  -f, --foreground  Stay in the terminal, show events and ask about new pairings
                    (Ctrl+C to stop). Same as ` + "`fileswap listen`" + `.
  --dir <path>      Swap folder for this run (default: init --dir, else ~/FileSwap)

Check it with ` + "`fileswap status`" + `; stop it with ` + "`fileswap stop`" + `.
`,
		"help.listen": "Usage: fileswap listen [--dir <path>]\n\nSame as `fileswap start --foreground`: online in this terminal, with the events\non screen and questions about new pairings. Ctrl+C to stop.\n",
		"help.service": `Usage: fileswap service install|remove

install   Writes a systemd user unit (~/.config/systemd/user/fileswap.service)
          for this binary and identity, then enables and starts it: the node
          comes online at every login and is restarted if it crashes. A node
          started with ` + "`fileswap start`" + ` is handed over to the service.
remove    Stops and disables the service and deletes that unit.

To also start at boot without logging in: sudo loginctl enable-linger $USER
Log: journalctl --user -u fileswap -f. ` + "`status`" + ` and ` + "`stop`" + ` work as usual
(` + "`stop`" + ` keeps it stopped until the next login or ` + "`systemctl --user start fileswap`" + `).
`,
		"service.noSystemd":     "systemd (systemctl) was not found; run the node with `fileswap start` instead",
		"service.wrote":         "Wrote %s",
		"service.stoppedManual": "Stopped the node started with `fileswap start`; systemd takes over.",
		"service.installed":     "FileSwap now runs as a systemd user service (starts at login, restarts if it crashes).\nCheck it with `fileswap status`; follow the log with `journalctl --user -u fileswap -f`.",
		"service.linger":        "To also start it at boot without logging in, run once: sudo loginctl enable-linger %s",
		"service.notOurs":       "%s was not written by fileswap; leaving it in place",
		"service.removed":       "The FileSwap service is stopped and removed.",
		"help.status":           "Usage: fileswap status\n\nShows whether this node is online (started with `fileswap start`), its device\ncode, server, swap folder and a table of the paired devices.\nExits with code 3 when it is not running.\n",
		"help.stop":             "Usage: fileswap stop\n\nTakes this node offline (started with `start` in the background or the foreground).\n",
		"help.allow":            "Usage: fileswap allow <code>\n\nPairing requests from this device code will be accepted without asking (also by `fileswap start` in the background).\n",
		"help.unpair":           "Usage: fileswap unpair <node>\n\nRemoves a paired device on both sides. If the server cannot be reached,\nit is removed here only.\n",
		"help.version":          "Usage: fileswap version\n",
		"help.help":             "Usage: fileswap help [command]\n",

		"err.unknownCommand": "unknown command %q (run `fileswap --help`)",
		"err.notInit":        "this node has no identity yet; run: fileswap init --name <name>",
		"err.nameRequired":   "--name is required the first time",
		"err.badCode":        "%q is not a valid device code (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)",
		"err.badServer":      "invalid server URL %q",
		"err.args":           "wrong arguments",

		"nerr.OFFLINE":           "cannot reach the FileSwap server %s",
		"nerr.TIMEOUT":           "no answer from the other device within 2 minutes",
		"nerr.REJECTED":          "the other device rejected the pairing",
		"nerr.SELF":              "that is this node's own code",
		"nerr.ALREADY_PAIRED":    "already paired with %s",
		"nerr.NOT_FOUND":         "no paired device matches %q",
		"nerr.AMBIGUOUS":         "%q matches several paired devices; use more of the code",
		"nerr.BUSY":              "another pairing request is already waiting for an answer",
		"nerr.REPLACED":          "this identity connected from somewhere else (another `start` or command), so this one went offline",
		"nerr.ALREADY_RUNNING":   "already running (PID %s); stop it with `fileswap stop`",
		"nerr.DEVICE_OFFLINE":    "that device is not online (open FileSwap on it, or run `fileswap start` there)",
		"nerr.TOO_MANY_REQUESTS": "that device has too many pending pairing requests",
		"nerr.RATE_LIMITED":      "too many messages; wait a minute and try again",
		"nerr.NOT_REGISTERED":    "not registered with the server yet; try again",
		"nerr.INTERNAL":          "internal error: %s",

		"init.created":      "Identity created.",
		"init.updated":      "Identity updated (device code unchanged).",
		"init.phi":          "Note: do not use FileSwap for patient or medical data (PHI) — transfers are not end-to-end encrypted yet.",
		"peers.none":        "None yet. Pair one with `fileswap sync <code>`, or with `fileswap start -f` and the web app.",
		"peers.unreachable": "Cannot reach %s; showing the saved list without online status.",
		"ui.code":           "Device code — type it in the web app to pair",
		"id.qr":             "Scan to pair from a phone (the node must be online: fileswap start):",
		"ui.server":         "Server",
		"ui.folder":         "Folder",
		"ui.link":           "Link",
		"ui.config":         "Config",
		"ui.log":            "Log",
		"ui.running":        "Running",
		"ui.runningValue":   "%s · PID %d · fileswap %s",
		"ui.paired":         "Paired devices",
		"ui.col.status":     "STATUS",
		"ui.col.name":       "NAME",
		"ui.col.code":       "CODE",
		"ui.online":         "online",
		"ui.offline":        "offline",
		"ui.connecting":     "connecting",
		"ui.unknown":        "unknown",

		"start.fgTitle":         "online in this terminal (Ctrl+C to stop)",
		"start.fgHint":          "No port is opened. Events:",
		"start.hint":            "Running in the background. `fileswap status` to check it, `fileswap stop` to stop it.",
		"start.already":         "Already running. `fileswap stop` to stop it.",
		"start.stillConnecting": "Still connecting to the server; check again with `fileswap status`.",
		"start.failed":          "could not start in the background (log: %s):\n%s",
		"status.notRunning":     "Not running. Start it with `fileswap start`.",
		"stop.done":             "%s is offline.",
		"stop.notRunning":       "%s is not running.",
		"log.started":           "Started %s (%s), saving files to %s",
		"log.stopped":           "Offline.",

		"allow.added":  "Pairing requests from %s will be accepted automatically.",
		"allow.exists": "%s is already allowed.",

		"sync.sent":        "Pairing request sent to %s — accept it on the other device (waiting up to 2 min)…",
		"sync.paired":      "Paired with %s (%s).",
		"unpair.done":      "Unpaired %s.",
		"unpair.localOnly": "Removed %s here; the server could not be reached, so the other device still lists this node.",

		"prompt.ask":        "  Pair with %s (%s)? [y/N] ",
		"prompt.background": "running in the background, so it cannot ask: bring it back with `fg`, or run `fileswap allow <code>` and retry.",

		"log.online":           "Online at %s",
		"log.offline":          "Disconnected (%s); reconnecting…",
		"log.connectFailed":    "Cannot reach %s (%s); retrying…",
		"log.peerOnline":       "%s is online",
		"log.peerOffline":      "%s went offline",
		"log.pairRequest":      "Pairing request from %s",
		"log.autoAccept":       "Accepted pairing from %s (in the allow list)",
		"log.autoAcceptPaired": "Accepted pairing from %s (already paired)",
		"log.rejectNotAllowed": "Rejected pairing from %s: not in the allow list (run `fileswap allow %s` and ask them to retry)",
		"log.rejected":         "Rejected pairing from %s",
		"log.acceptFailed":     "Could not complete pairing with %s: %s",
		"log.paired":           "Paired with %s",
		"log.requestSent":      "Pairing request sent to %s",
		"log.unpaired":         "Unpaired %s",
		"log.unpairedBy":       "%s removed the pairing",
		"log.serverError":      "Server error %s: %s",

		"err.notDir":  "%s exists and is not a folder",
		"tfr.done":    "Sent %s (%s) to %s — verified, SHA-256 %s…",
		"tfr.doneZip": "Sent %d files as %s (%s) to %s — verified, SHA-256 %s…",
		"tfr.many":    "%d items",

		"nerr.NOT_A_FILE":      "%s is not a file or a folder",
		"nerr.NOTHING_TO_SEND": "%s has no files to send",
		"nerr.PEER_ABORTED":    "the other device cancelled the transfer (%s)",
		"nerr.CONNECTION_LOST": "the connection with the other device was lost",
		"nerr.CONNECT_TIMEOUT": "could not connect directly with %s in 30 s (a firewall may be blocking WebRTC/UDP)",
		"nerr.HASH_MISMATCH":   "the file arrived corrupted (SHA-256 mismatch); nothing was saved",
		"nerr.SIZE_MISMATCH":   "the other device did not receive the whole file",
		"nerr.WRITE_FAILED":    "the other device could not save the file",
		"nerr.NO_SPACE":        "not enough disk space on the other device",
		"nerr.STALLED":         "the transfer stopped making progress for 30 s",
		"nerr.NO_ACK":          "the other device did not confirm the file within 60 s",
		"nerr.CANCELLED":       "transfer cancelled",
		"abort.CANCELLED":      "cancelled",
		"abort.BUSY":           "too many transfers at once",
		"abort.NOT_RECEIVING":  "it is not receiving files; run `fileswap start` there",
		"abort.ERROR":          "error",

		"log.receiving":         "Receiving %s (%s) from %s…",
		"log.receivingZip":      "Receiving %s (%d files, ~%s) from %s…",
		"log.received":          "Saved %s (%s) from %s — SHA-256 %s… verified",
		"log.receiveFailed":     "Transfer from %s failed: %s",
		"log.receiveFailedFile": "Could not receive %s from %s: %s",
		"log.receiveBusy":       "Refused a transfer from %s: too many at once",
		"log.connecting":        "Connecting to %s…",
		"log.sending":           "Sending %s (%s) to %s…",
		"log.sendingZip":        "Sending %s (%d files, %s) to %s…",
		"log.sent":              "Sent %s (%s) to %s — SHA-256 %s… verified",
		"log.sendFailed":        "Could not send %s to %s: %s",
		"log.sendCancelled":     "Cancelled sending %s to %s",
	},
	"es": {
		"usage": `fileswap — envía archivos entre navegadores y servidores Linux, de igual a igual

Uso:
  fileswap [--config <dir>] <comando> [opciones]

Comandos:
  init     --name <nombre> [--server <url>] [--dir <ruta>]
                                             Crea la identidad de este nodo
  id       [--qr]                            Muestra el nombre, el código y el enlace de vinculación (--qr: como QR)
  sync     <código>                          Se vincula con un dispositivo (debe aceptar)       (alias: -sync)
  peers                                      Lista los dispositivos vinculados y quién está en línea
  unpair   <nodo>                            Desvincula un dispositivo en ambos lados
  start    [-f] [--dir <ruta>]               Se pone en línea en segundo plano para recibir archivos (-f: primer plano)
  status                                     Indica si está en línea y muestra los dispositivos vinculados
  stop                                       Se desconecta
  service  install|remove                    Lo instala como servicio de usuario de systemd (arranca al iniciar)
  allow    <código>                          Acepta la vinculación de ese dispositivo sin preguntar
  tfr      <nodo> <ruta>...                  Envía archivos o carpetas a un dispositivo vinculado (varios → un ZIP) (alias: -tfr, send)
  version                                    Muestra la versión
  help     [comando]                         Muestra esta ayuda, o la de un comando             (alias: --help, -h)

<nodo> es un código de dispositivo, un prefijo único de él, o el nombre del dispositivo.
<código> es el código completo del dispositivo (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
` + "`fileswap listen`" + ` equivale a ` + "`fileswap start -f`" + `.

Los archivos recibidos van a la carpeta de swap: --dir, si no la fijada con
` + "`init --dir`" + `, si no ~/FileSwap.

fileswap nunca abre puertos: solo se conecta hacia el servidor FileSwap y,
para las transferencias, directamente al otro dispositivo.

Opciones:
  --config <dir>   Directorio de configuración (por defecto: $FILESWAP_CONFIG, si no $XDG_CONFIG_HOME/fileswap, si no ~/.config/fileswap)
`,
		"help.init": `Uso: fileswap init --name <nombre> [--server <url>] [--dir <ruta>] [--force]

Crea la identidad de este nodo: un secreto privado guardado en el directorio de
configuración y el código público que se deriva de él. Si vuelves a ejecutar
init, el código se mantiene y solo cambian el nombre o el servidor; --force crea
un código nuevo (los dispositivos vinculados tendrán que vincularse de nuevo).

  --name     Nombre que verán los otros dispositivos (obligatorio la primera vez)
  --server   Servidor FileSwap, p. ej. ws://192.168.1.10:7341 o https://fileswap.ejemplo.com
             (por defecto: ws://localhost:7341)
  --dir      Carpeta de swap donde se guarda lo recibido (por defecto: ~/FileSwap)
  --force    Reemplaza la identidad por una nueva
`,
		"help.id":    "Uso: fileswap id [--qr]\n\nMuestra el nombre, el código del dispositivo, el servidor, la carpeta de swap\ny el enlace para vincular un navegador.\n\n  --qr   Dibuja además el enlace como código QR: escanéalo con el móvil para abrir\n         FileSwap y vincularte (el nodo debe estar en línea: fileswap start).\n",
		"help.peers": "Uso: fileswap peers\n\nLista los dispositivos vinculados y si cada uno está en línea ahora.\n",
		"help.sync": `Uso: fileswap sync <código>

Envía una solicitud de vinculación al dispositivo con ese código y espera (hasta
2 min) a que la acepten allí. El otro dispositivo debe estar en línea: un
navegador con FileSwap abierto, o un nodo con ` + "`fileswap start`" + `.
`,
		"help.tfr": `Uso: fileswap tfr <nodo> <ruta>...

Envía archivos directamente a un dispositivo vinculado (de igual a igual,
WebRTC) y espera a que el otro lado confirme que recibió todos los bytes
(se comprueba el SHA-256).

Un archivo se envía tal cual. Varios archivos, o carpetas, se empaquetan en
un único ZIP mientras se envían —en este equipo, nunca en el servidor— y
llegan como un .zip ("<carpeta>.zip" si es una sola carpeta). Fotos, vídeos
y otros formatos ya comprimidos se guardan tal cual; el resto se comprime.

El otro dispositivo debe estar en línea: un navegador con FileSwap abierto, o
un nodo con ` + "`fileswap start`" + `. Ctrl+C cancela la transferencia en ambos lados.

<nodo> es un código de dispositivo, un prefijo único de él, o el nombre del dispositivo.
`,
		"help.start": `Uso: fileswap start [-f] [--dir <ruta>]

Pone este nodo en línea en segundo plano y te devuelve la terminal: los
navegadores y nodos vinculados pueden enviarle archivos, que se guardan en la
carpeta de swap (sin sobrescribir). Los eventos van a
~/.local/state/fileswap/listen.log. No abre ningún puerto: solo se conecta
hacia el servidor FileSwap.

En segundo plano solo se aceptan vinculaciones de dispositivos ya vinculados o
en la lista de permitidos. Para vincular uno nuevo, ejecuta
` + "`fileswap sync <código>`" + ` y acéptalo en el navegador, o antes ` + "`fileswap allow <código>`" + `.

  -f, --foreground  Se queda en la terminal, muestra los eventos y pregunta por
                    vinculaciones nuevas (Ctrl+C para salir). Igual que ` + "`fileswap listen`" + `.
  --dir <ruta>      Carpeta de swap para esta ejecución (por defecto: init --dir, si no ~/FileSwap)

Revísalo con ` + "`fileswap status`" + `; detenlo con ` + "`fileswap stop`" + `.
`,
		"help.listen": "Uso: fileswap listen [--dir <ruta>]\n\nIgual que `fileswap start --foreground`: en línea en esta terminal, con los\neventos en pantalla y preguntas sobre vinculaciones nuevas. Ctrl+C para salir.\n",
		"help.service": `Uso: fileswap service install|remove

install   Escribe una unidad de usuario de systemd (~/.config/systemd/user/fileswap.service)
          para este binario e identidad, y la activa: el nodo se pone en línea
          en cada inicio de sesión y se reinicia si se cae. Un nodo iniciado
          con ` + "`fileswap start`" + ` pasa a manos del servicio.
remove    Detiene y desactiva el servicio y borra esa unidad.

Para que arranque también al encender sin iniciar sesión: sudo loginctl enable-linger $USER
Registro: journalctl --user -u fileswap -f. ` + "`status`" + ` y ` + "`stop`" + ` funcionan igual
(` + "`stop`" + ` lo deja detenido hasta el próximo inicio de sesión o ` + "`systemctl --user start fileswap`" + `).
`,
		"service.noSystemd":     "no se encontró systemd (systemctl); usa `fileswap start` para poner el nodo en línea",
		"service.wrote":         "Escrito %s",
		"service.stoppedManual": "Se detuvo el nodo iniciado con `fileswap start`; ahora lo gestiona systemd.",
		"service.installed":     "FileSwap corre ahora como servicio de usuario de systemd (arranca al iniciar sesión y se reinicia si se cae).\nRevísalo con `fileswap status`; sigue el registro con `journalctl --user -u fileswap -f`.",
		"service.linger":        "Para que arranque también al encender sin iniciar sesión, ejecuta una vez: sudo loginctl enable-linger %s",
		"service.notOurs":       "%s no lo escribió fileswap; se deja como está",
		"service.removed":       "El servicio de FileSwap está detenido y eliminado.",
		"help.status":           "Uso: fileswap status\n\nIndica si este nodo está en línea (iniciado con `fileswap start`), su código,\nservidor, carpeta de swap y una tabla con los dispositivos vinculados.\nTermina con código 3 si no está corriendo.\n",
		"help.stop":             "Uso: fileswap stop\n\nDesconecta este nodo (iniciado con `start`, en segundo o primer plano).\n",
		"help.allow":            "Uso: fileswap allow <código>\n\nLas solicitudes de vinculación de ese código se aceptarán sin preguntar (también con `fileswap start` en segundo plano).\n",
		"help.unpair":           "Uso: fileswap unpair <nodo>\n\nDesvincula un dispositivo en ambos lados. Si no se puede llegar al servidor,\nse elimina solo aquí.\n",
		"help.version":          "Uso: fileswap version\n",
		"help.help":             "Uso: fileswap help [comando]\n",

		"err.unknownCommand": "comando desconocido %q (ejecuta `fileswap --help`)",
		"err.notInit":        "este nodo aún no tiene identidad; ejecuta: fileswap init --name <nombre>",
		"err.nameRequired":   "--name es obligatorio la primera vez",
		"err.badCode":        "%q no es un código de dispositivo válido (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)",
		"err.badServer":      "URL de servidor no válida %q",
		"err.args":           "argumentos incorrectos",

		"nerr.OFFLINE":           "no se puede llegar al servidor FileSwap %s",
		"nerr.TIMEOUT":           "el otro dispositivo no respondió en 2 minutos",
		"nerr.REJECTED":          "el otro dispositivo rechazó la vinculación",
		"nerr.SELF":              "ese es el código de este mismo nodo",
		"nerr.ALREADY_PAIRED":    "ya estás vinculado con %s",
		"nerr.NOT_FOUND":         "ningún dispositivo vinculado coincide con %q",
		"nerr.AMBIGUOUS":         "%q coincide con varios dispositivos vinculados; usa más caracteres del código",
		"nerr.BUSY":              "ya hay otra solicitud de vinculación esperando respuesta",
		"nerr.REPLACED":          "esta identidad se conectó desde otro lugar (otro `start` u otro comando), así que esta se desconectó",
		"nerr.ALREADY_RUNNING":   "ya está corriendo (PID %s); detenlo con `fileswap stop`",
		"nerr.DEVICE_OFFLINE":    "ese dispositivo no está en línea (abre FileSwap en él, o ejecuta `fileswap start` allí)",
		"nerr.TOO_MANY_REQUESTS": "ese dispositivo tiene demasiadas solicitudes pendientes",
		"nerr.RATE_LIMITED":      "demasiados mensajes; espera un minuto y vuelve a intentarlo",
		"nerr.NOT_REGISTERED":    "aún no registrado en el servidor; vuelve a intentarlo",
		"nerr.INTERNAL":          "error interno: %s",

		"init.created":      "Identidad creada.",
		"init.updated":      "Identidad actualizada (el código no cambia).",
		"init.phi":          "Aviso: no uses FileSwap con datos de pacientes ni información médica (PHI); las transferencias aún no tienen cifrado de extremo a extremo.",
		"peers.none":        "Ninguno todavía. Vincula uno con `fileswap sync <código>`, o con `fileswap start -f` y la app web.",
		"peers.unreachable": "No se puede llegar a %s; se muestra la lista guardada sin estado en línea.",
		"ui.code":           "Código del dispositivo — escríbelo en la app web para vincular",
		"id.qr":             "Escanéalo con el móvil para vincularte (el nodo debe estar en línea: fileswap start):",
		"ui.server":         "Servidor",
		"ui.folder":         "Carpeta",
		"ui.link":           "Enlace",
		"ui.config":         "Config",
		"ui.log":            "Registro",
		"ui.running":        "Activo",
		"ui.runningValue":   "%s · PID %d · fileswap %s",
		"ui.paired":         "Dispositivos vinculados",
		"ui.col.status":     "ESTADO",
		"ui.col.name":       "NOMBRE",
		"ui.col.code":       "CÓDIGO",
		"ui.online":         "en línea",
		"ui.offline":        "sin conexión",
		"ui.connecting":     "conectando",
		"ui.unknown":        "desconocido",

		"start.fgTitle":         "en línea en esta terminal (Ctrl+C para salir)",
		"start.fgHint":          "No se abre ningún puerto. Eventos:",
		"start.hint":            "Corriendo en segundo plano. `fileswap status` para revisarlo, `fileswap stop` para detenerlo.",
		"start.already":         "Ya estaba corriendo. `fileswap stop` para detenerlo.",
		"start.stillConnecting": "Todavía conectándose al servidor; revisa de nuevo con `fileswap status`.",
		"start.failed":          "no se pudo iniciar en segundo plano (registro: %s):\n%s",
		"status.notRunning":     "No está corriendo. Inícialo con `fileswap start`.",
		"stop.done":             "%s está desconectado.",
		"stop.notRunning":       "%s no está corriendo.",
		"log.started":           "Iniciado %s (%s), los archivos se guardan en %s",
		"log.stopped":           "Desconectado.",

		"allow.added":  "Las solicitudes de vinculación de %s se aceptarán automáticamente.",
		"allow.exists": "%s ya estaba permitido.",

		"sync.sent":        "Solicitud enviada a %s; acéptala en el otro dispositivo (esperando hasta 2 min)…",
		"sync.paired":      "Vinculado con %s (%s).",
		"unpair.done":      "%s desvinculado.",
		"unpair.localOnly": "%s eliminado aquí; no se pudo llegar al servidor, así que el otro dispositivo todavía tiene este nodo en su lista.",

		"prompt.ask":        "  ¿Vincular con %s (%s)? [s/N] ",
		"prompt.background": "está en segundo plano y no puede preguntar: tráelo con `fg`, o ejecuta `fileswap allow <código>` y vuelve a intentarlo.",

		"log.online":           "En línea en %s",
		"log.offline":          "Desconectado (%s); reconectando…",
		"log.connectFailed":    "No se puede llegar a %s (%s); reintentando…",
		"log.peerOnline":       "%s está en línea",
		"log.peerOffline":      "%s se desconectó",
		"log.pairRequest":      "Solicitud de vinculación de %s",
		"log.autoAccept":       "Vinculación aceptada de %s (en la lista de permitidos)",
		"log.autoAcceptPaired": "Vinculación aceptada de %s (ya estaba vinculado)",
		"log.rejectNotAllowed": "Vinculación rechazada de %s: no está en la lista de permitidos (ejecuta `fileswap allow %s` y pide que lo intente de nuevo)",
		"log.rejected":         "Vinculación rechazada de %s",
		"log.acceptFailed":     "No se pudo completar la vinculación con %s: %s",
		"log.paired":           "Vinculado con %s",
		"log.requestSent":      "Solicitud de vinculación enviada a %s",
		"log.unpaired":         "%s desvinculado",
		"log.unpairedBy":       "%s eliminó la vinculación",
		"log.serverError":      "Error del servidor %s: %s",

		"err.notDir":  "%s existe y no es una carpeta",
		"tfr.done":    "%s (%s) enviado a %s; verificado, SHA-256 %s…",
		"tfr.doneZip": "%d archivos enviados como %s (%s) a %s; verificado, SHA-256 %s…",
		"tfr.many":    "%d elementos",

		"nerr.NOT_A_FILE":      "%s no es un archivo ni una carpeta",
		"nerr.NOTHING_TO_SEND": "%s no tiene archivos que enviar",
		"nerr.PEER_ABORTED":    "el otro dispositivo canceló la transferencia (%s)",
		"nerr.CONNECTION_LOST": "se perdió la conexión con el otro dispositivo",
		"nerr.CONNECT_TIMEOUT": "no se pudo conectar directamente con %s en 30 s (un firewall puede estar bloqueando WebRTC/UDP)",
		"nerr.HASH_MISMATCH":   "el archivo llegó dañado (el SHA-256 no coincide); no se guardó nada",
		"nerr.SIZE_MISMATCH":   "el otro dispositivo no recibió el archivo completo",
		"nerr.WRITE_FAILED":    "el otro dispositivo no pudo guardar el archivo",
		"nerr.NO_SPACE":        "no hay espacio suficiente en el disco del otro dispositivo",
		"nerr.STALLED":         "la transferencia dejó de avanzar durante 30 s",
		"nerr.NO_ACK":          "el otro dispositivo no confirmó el archivo en 60 s",
		"nerr.CANCELLED":       "transferencia cancelada",
		"abort.CANCELLED":      "cancelada",
		"abort.BUSY":           "demasiadas transferencias a la vez",
		"abort.NOT_RECEIVING":  "no está recibiendo archivos; ejecuta `fileswap start` allí",
		"abort.ERROR":          "error",

		"log.receiving":         "Recibiendo %s (%s) de %s…",
		"log.receivingZip":      "Recibiendo %s (%d archivos, ~%s) de %s…",
		"log.received":          "Guardado %s (%s) de %s; SHA-256 %s… verificado",
		"log.receiveFailed":     "Falló la transferencia de %s: %s",
		"log.receiveFailedFile": "No se pudo recibir %s de %s: %s",
		"log.receiveBusy":       "Transferencia de %s rechazada: demasiadas a la vez",
		"log.connecting":        "Conectando con %s…",
		"log.sending":           "Enviando %s (%s) a %s…",
		"log.sendingZip":        "Enviando %s (%d archivos, %s) a %s…",
		"log.sent":              "%s (%s) enviado a %s; SHA-256 %s… verificado",
		"log.sendFailed":        "No se pudo enviar %s a %s: %s",
		"log.sendCancelled":     "Envío de %s a %s cancelado",
	},
}
