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
  id                                         Show this node's name, code and pairing link
  sync     <code>                            Pair with a device (it must accept)    (alias: -sync)
  peers                                      List paired devices and who is online
  unpair   <node>                            Remove a paired device on both sides
  allow    <code>                            Let listen accept pairing from this device without asking
  listen   [-d] [--dir <path>]               Go online to receive files and pairing requests (-d: background)
  status                                     Show whether listen is running
  stop                                       Take a running listen offline
  tfr      <node> <path>                     Send a file to a paired device         (alias: -tfr, send)
  version                                    Print the version
  help     [command]                         Show this help, or a command's help    (alias: --help, -h)

<node> is a device code, a unique prefix of it, or the device's name.
<code> is the full device code (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).

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
  --dir      Swap folder where listen saves received files (default: ~/FileSwap)
  --force    Replace the identity with a new one
`,
		"help.id":    "Usage: fileswap id\n\nShows this node's name, device code, server and the link that pairs a browser with it.\n",
		"help.peers": "Usage: fileswap peers\n\nLists the paired devices and whether each one is online now.\n",
		"help.sync": `Usage: fileswap sync <code>

Sends a pairing request to the device with that code and waits (up to 2 min)
until it is accepted there. The other device must be online: a browser with
FileSwap open, or a node running ` + "`fileswap listen`" + `.
`,
		"help.tfr": `Usage: fileswap tfr <node> <path>

Sends a file straight to a paired device (peer to peer, WebRTC) and waits
until the other side confirms it received every byte (SHA-256 checked).
The other device must be online: a browser with FileSwap open, or a node
running ` + "`fileswap listen`" + `. Ctrl+C cancels the transfer on both sides.

<node> is a device code, a unique prefix of it, or the device's name.
`,
		"help.listen": `Usage: fileswap listen [-d] [--dir <path>]

Keeps this node online so paired browsers and nodes can send it files, and
new ones can pair with it. Files are saved in the swap folder, never
overwriting existing ones. No port is opened: the node only keeps an
outgoing connection to the FileSwap server.

Pairing requests from devices in the allow list (` + "`fileswap allow`" + `) or already
paired are accepted automatically. Others are asked on the terminal, or
rejected when running in the background.

  -d, --daemon   Run in the background; log to ~/.local/state/fileswap/listen.log
  --dir <path>   Swap folder for this run (default: the one set with init --dir, else ~/FileSwap)

Stop it with Ctrl+C, or with ` + "`fileswap stop`" + ` from another terminal.
`,
		"help.status":  "Usage: fileswap status\n\nShows whether `listen` is running, its connection and the paired devices.\nExits with code 3 when it is not running.\n",
		"help.stop":    "Usage: fileswap stop\n\nTakes a running `listen` offline (foreground or background).\n",
		"help.allow":   "Usage: fileswap allow <code>\n\nPairing requests from this device code will be accepted by `listen` without asking.\n",
		"help.unpair":  "Usage: fileswap unpair <node>\n\nRemoves a paired device on both sides. If the server cannot be reached,\nit is removed here only.\n",
		"help.version": "Usage: fileswap version\n",
		"help.help":    "Usage: fileswap help [command]\n",

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
		"nerr.REPLACED":          "this identity connected from somewhere else (another listen or command), so this one went offline",
		"nerr.ALREADY_RUNNING":   "listen is already running (PID %s); stop it with `fileswap stop`",
		"nerr.DEVICE_OFFLINE":    "that device is not online (open FileSwap on it, or run `fileswap listen` there)",
		"nerr.TOO_MANY_REQUESTS": "that device has too many pending pairing requests",
		"nerr.RATE_LIMITED":      "too many messages; wait a minute and try again",
		"nerr.NOT_REGISTERED":    "not registered with the server yet; try again",
		"nerr.INTERNAL":          "internal error: %s",

		"init.created":      "Identity created.",
		"init.updated":      "Identity updated (device code unchanged).",
		"init.phi":          "Note: do not use FileSwap for patient or medical data (PHI) — transfers are not end-to-end encrypted yet.",
		"id.name":           "Name:    %s",
		"id.code":           "Code:    %s",
		"id.server":         "Server:  %s",
		"id.link":           "Pairing link (open it in a browser): %s",
		"id.config":         "Config:  %s",
		"peers.none":        "No paired devices. Pair one with `fileswap sync <code>`, or run `fileswap listen` and pair from the web app.",
		"peers.unreachable": "Cannot reach %s; showing the saved list without online status.",
		"peer.online":       "online",
		"peer.offline":      "offline",
		"allow.added":       "Pairing requests from %s will be accepted automatically.",
		"allow.exists":      "%s is already allowed.",

		"sync.sent":        "Pairing request sent to %s — accept it on the other device (waiting up to 2 min)…",
		"sync.paired":      "Paired with %s (%s).",
		"unpair.done":      "Unpaired %s.",
		"unpair.localOnly": "Removed %s here; the server could not be reached, so the other device still lists this node.",

		"status.notRunning": "Offline: listen is not running for %s (%s). Start it with `fileswap listen`.",
		"status.head":       "%s — %s (%s)",
		"status.online":     "Online",
		"status.connecting": "Connecting",
		"status.server":     "Server: %s",
		"status.pid":        "PID %d, up %s, fileswap %s",
		"stop.done":         "Offline.",
		"stop.notRunning":   "listen is not running.",

		"listen.banner":     "%s (%s) going online at %s.",
		"listen.hint":       "No port is opened. Stop with Ctrl+C or `fileswap stop`.",
		"listen.hintDaemon": "No port is opened. Stop with `fileswap stop`. Pairing requests not in the allow list are rejected.",
		"listen.stopped":    "Offline.",
		"listen.bg":         "%s is online in the background (PID %d). Log: %s\nCheck it with `fileswap status`; stop it with `fileswap stop`.",
		"listen.bgFailed":   "listen did not start in the background (log: %s):\n%s",
		"prompt.ask":        "  Pair with %s (%s)? [y/N] ",

		"log.online":           "Online at %s — waiting for files and pairing requests",
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

		"id.dir":       "Folder:  %s",
		"err.notDir":   "%s exists and is not a folder",
		"tfr.done":     "Sent %s (%s) to %s — verified, SHA-256 %s…",
		"listen.inbox": "Saving received files to %s",

		"nerr.NOT_A_FILE":      "%s is not a regular file (folders are not supported yet)",
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
		"abort.NOT_RECEIVING":  "it is not receiving files; run `fileswap listen` there",
		"abort.ERROR":          "error",

		"log.receiving":         "Receiving %s (%s) from %s…",
		"log.received":          "Saved %s (%s) from %s — SHA-256 %s… verified",
		"log.receiveFailed":     "Transfer from %s failed: %s",
		"log.receiveFailedFile": "Could not receive %s from %s: %s",
		"log.receiveBusy":       "Refused a transfer from %s: too many at once",
		"log.connecting":        "Connecting to %s…",
		"log.sending":           "Sending %s (%s) to %s…",
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
  id                                         Muestra el nombre, el código y el enlace de vinculación
  sync     <código>                          Se vincula con un dispositivo (debe aceptar)       (alias: -sync)
  peers                                      Lista los dispositivos vinculados y quién está en línea
  unpair   <nodo>                            Desvincula un dispositivo en ambos lados
  allow    <código>                          listen aceptará la vinculación de ese dispositivo sin preguntar
  listen   [-d] [--dir <ruta>]               Se pone en línea para recibir archivos y solicitudes (-d: segundo plano)
  status                                     Indica si listen está corriendo
  stop                                       Desconecta un listen en marcha
  tfr      <nodo> <ruta>                     Envía un archivo a un dispositivo vinculado        (alias: -tfr, send)
  version                                    Muestra la versión
  help     [comando]                         Muestra esta ayuda, o la de un comando             (alias: --help, -h)

<nodo> es un código de dispositivo, un prefijo único de él, o el nombre del dispositivo.
<código> es el código completo del dispositivo (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).

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
  --dir      Carpeta de swap donde listen guarda lo recibido (por defecto: ~/FileSwap)
  --force    Reemplaza la identidad por una nueva
`,
		"help.id":    "Uso: fileswap id\n\nMuestra el nombre, el código del dispositivo, el servidor y el enlace para vincular un navegador.\n",
		"help.peers": "Uso: fileswap peers\n\nLista los dispositivos vinculados y si cada uno está en línea ahora.\n",
		"help.sync": `Uso: fileswap sync <código>

Envía una solicitud de vinculación al dispositivo con ese código y espera (hasta
2 min) a que la acepten allí. El otro dispositivo debe estar en línea: un
navegador con FileSwap abierto, o un nodo con ` + "`fileswap listen`" + `.
`,
		"help.tfr": `Uso: fileswap tfr <nodo> <ruta>

Envía un archivo directamente a un dispositivo vinculado (de igual a igual,
WebRTC) y espera a que el otro lado confirme que recibió todos los bytes
(se comprueba el SHA-256). El otro dispositivo debe estar en línea: un
navegador con FileSwap abierto, o un nodo con ` + "`fileswap listen`" + `.
Ctrl+C cancela la transferencia en ambos lados.

<nodo> es un código de dispositivo, un prefijo único de él, o el nombre del dispositivo.
`,
		"help.listen": `Uso: fileswap listen [-d] [--dir <ruta>]

Mantiene este nodo en línea para que los navegadores y nodos vinculados le
envíen archivos, y para que otros nuevos se vinculen. Los archivos se guardan
en la carpeta de swap, sin sobrescribir los existentes. No abre ningún
puerto: solo mantiene una conexión saliente con el servidor FileSwap.

Las solicitudes de dispositivos en la lista de permitidos (` + "`fileswap allow`" + `) o ya
vinculados se aceptan solas. Las demás se preguntan en la terminal, o se
rechazan si corre en segundo plano.

  -d, --daemon   Corre en segundo plano; registro en ~/.local/state/fileswap/listen.log
  --dir <ruta>   Carpeta de swap para esta ejecución (por defecto: la fijada con init --dir, si no ~/FileSwap)

Se detiene con Ctrl+C, o con ` + "`fileswap stop`" + ` desde otra terminal.
`,
		"help.status":  "Uso: fileswap status\n\nIndica si `listen` está corriendo, su conexión y los dispositivos vinculados.\nTermina con código 3 si no está corriendo.\n",
		"help.stop":    "Uso: fileswap stop\n\nDesconecta un `listen` en marcha (en primer o segundo plano).\n",
		"help.allow":   "Uso: fileswap allow <código>\n\n`listen` aceptará sin preguntar las solicitudes de vinculación de ese código.\n",
		"help.unpair":  "Uso: fileswap unpair <nodo>\n\nDesvincula un dispositivo en ambos lados. Si no se puede llegar al servidor,\nse elimina solo aquí.\n",
		"help.version": "Uso: fileswap version\n",
		"help.help":    "Uso: fileswap help [comando]\n",

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
		"nerr.REPLACED":          "esta identidad se conectó desde otro lugar (otro listen u otro comando), así que esta se desconectó",
		"nerr.ALREADY_RUNNING":   "listen ya está corriendo (PID %s); detenlo con `fileswap stop`",
		"nerr.DEVICE_OFFLINE":    "ese dispositivo no está en línea (abre FileSwap en él, o ejecuta `fileswap listen` allí)",
		"nerr.TOO_MANY_REQUESTS": "ese dispositivo tiene demasiadas solicitudes pendientes",
		"nerr.RATE_LIMITED":      "demasiados mensajes; espera un minuto y vuelve a intentarlo",
		"nerr.NOT_REGISTERED":    "aún no registrado en el servidor; vuelve a intentarlo",
		"nerr.INTERNAL":          "error interno: %s",

		"init.created":      "Identidad creada.",
		"init.updated":      "Identidad actualizada (el código no cambia).",
		"init.phi":          "Aviso: no uses FileSwap con datos de pacientes ni información médica (PHI); las transferencias aún no tienen cifrado de extremo a extremo.",
		"id.name":           "Nombre:   %s",
		"id.code":           "Código:   %s",
		"id.server":         "Servidor: %s",
		"id.link":           "Enlace de vinculación (ábrelo en un navegador): %s",
		"id.config":         "Config:   %s",
		"peers.none":        "No hay dispositivos vinculados. Vincula uno con `fileswap sync <código>`, o ejecuta `fileswap listen` y vincúlalo desde la app web.",
		"peers.unreachable": "No se puede llegar a %s; se muestra la lista guardada sin estado en línea.",
		"peer.online":       "en línea",
		"peer.offline":      "sin conexión",
		"allow.added":       "Las solicitudes de vinculación de %s se aceptarán automáticamente.",
		"allow.exists":      "%s ya estaba permitido.",

		"sync.sent":        "Solicitud enviada a %s; acéptala en el otro dispositivo (esperando hasta 2 min)…",
		"sync.paired":      "Vinculado con %s (%s).",
		"unpair.done":      "%s desvinculado.",
		"unpair.localOnly": "%s eliminado aquí; no se pudo llegar al servidor, así que el otro dispositivo todavía tiene este nodo en su lista.",

		"status.notRunning": "Sin conexión: listen no está corriendo para %s (%s). Inícialo con `fileswap listen`.",
		"status.head":       "%s — %s (%s)",
		"status.online":     "En línea",
		"status.connecting": "Conectando",
		"status.server":     "Servidor: %s",
		"status.pid":        "PID %d, activo hace %s, fileswap %s",
		"stop.done":         "Desconectado.",
		"stop.notRunning":   "listen no está corriendo.",

		"listen.banner":     "%s (%s) conectándose a %s.",
		"listen.hint":       "No se abre ningún puerto. Detén con Ctrl+C o `fileswap stop`.",
		"listen.hintDaemon": "No se abre ningún puerto. Detén con `fileswap stop`. Las solicitudes fuera de la lista de permitidos se rechazan.",
		"listen.stopped":    "Desconectado.",
		"listen.bg":         "%s está en línea en segundo plano (PID %d). Registro: %s\nRevísalo con `fileswap status`; detenlo con `fileswap stop`.",
		"listen.bgFailed":   "listen no arrancó en segundo plano (registro: %s):\n%s",
		"prompt.ask":        "  ¿Vincular con %s (%s)? [s/N] ",

		"log.online":           "En línea en %s; esperando archivos y solicitudes de vinculación",
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

		"id.dir":       "Carpeta:  %s",
		"err.notDir":   "%s existe y no es una carpeta",
		"tfr.done":     "%s (%s) enviado a %s; verificado, SHA-256 %s…",
		"listen.inbox": "Los archivos recibidos se guardan en %s",

		"nerr.NOT_A_FILE":      "%s no es un archivo normal (todavía no se pueden enviar carpetas)",
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
		"abort.NOT_RECEIVING":  "no está recibiendo archivos; ejecuta `fileswap listen` allí",
		"abort.ERROR":          "error",

		"log.receiving":         "Recibiendo %s (%s) de %s…",
		"log.received":          "Guardado %s (%s) de %s; SHA-256 %s… verificado",
		"log.receiveFailed":     "Falló la transferencia de %s: %s",
		"log.receiveFailedFile": "No se pudo recibir %s de %s: %s",
		"log.receiveBusy":       "Transferencia de %s rechazada: demasiadas a la vez",
		"log.connecting":        "Conectando con %s…",
		"log.sending":           "Enviando %s (%s) a %s…",
		"log.sent":              "%s (%s) enviado a %s; SHA-256 %s… verificado",
		"log.sendFailed":        "No se pudo enviar %s a %s: %s",
		"log.sendCancelled":     "Envío de %s a %s cancelado",
	},
}
