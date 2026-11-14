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
  init     --name <name> [--server <url>]   Create this node's identity
  id                                         Show this node's name, code and pairing link
  peers                                      List paired devices
  sync     <code>                            Ask a device to pair with this node    (alias: -sync) *
  tfr      <node> <path>                     Send a file to a paired device         (alias: -tfr, send) *
  listen   [--dir <path>]                    Stay online to receive files and pairing requests *
  allow    <code>                            Accept pairing from this device without asking
  unpair   <node>                            Remove a paired device *
  version                                    Print the version
  help     [command]                         Show this help, or a command's help    (alias: --help, -h)

<node> is a device code, a unique prefix of it, or the device's name.
<code> is the full device code (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).

Options:
  --config <dir>   Config directory (default: $FILESWAP_CONFIG, else $XDG_CONFIG_HOME/fileswap, else ~/.config/fileswap)

* Not available yet in this version.
`,
		"help.init": `Usage: fileswap init --name <name> [--server <url>] [--force]

Creates this node's identity: a private secret stored in the config directory
and the public device code derived from it. Running init again keeps the code
and only updates the name or server; --force creates a new code (paired devices
will have to pair again).

  --name     Device name shown to other devices (required the first time)
  --server   FileSwap server, e.g. ws://192.168.1.10:7341 or https://fileswap.example.com
             (default: ws://localhost:7341)
  --force    Replace the identity with a new one
`,
		"help.id":      "Usage: fileswap id\n\nShows this node's name, device code, server and the link that pairs a browser with it.\n",
		"help.peers":   "Usage: fileswap peers\n\nLists the devices this node is paired with.\n",
		"help.sync":    "Usage: fileswap sync <code>\n\nSends a pairing request to the device with that code and waits until it accepts.\nThe other device must be online.\n",
		"help.tfr":     "Usage: fileswap tfr <node> <path>\n\nSends a file to a paired device. The other device must be online.\n",
		"help.listen":  "Usage: fileswap listen [--dir <path>]\n\nStays online: receives files into --dir (default ~/FileSwap) and handles pairing requests.\n",
		"help.allow":   "Usage: fileswap allow <code>\n\nPairing requests from this device code will be accepted by `listen` without asking.\n",
		"help.unpair":  "Usage: fileswap unpair <node>\n\nRemoves a paired device on both sides.\n",
		"help.version": "Usage: fileswap version\n",
		"help.help":    "Usage: fileswap help [command]\n",

		"err.unknownCommand": "unknown command %q (run `fileswap --help`)",
		"err.notInit":        "this node has no identity yet; run: fileswap init --name <name>",
		"err.nameRequired":   "--name is required the first time",
		"err.badCode":        "%q is not a valid device code (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)",
		"err.badServer":      "invalid server URL %q",
		"err.args":           "wrong arguments",
		"err.notYet":         "`%s` is not available yet in this version",

		"init.created": "Identity created.",
		"init.updated": "Identity updated (device code unchanged).",
		"init.phi":     "Note: do not use FileSwap for patient or medical data (PHI) — transfers are not end-to-end encrypted yet.",
		"id.name":      "Name:    %s",
		"id.code":      "Code:    %s",
		"id.server":    "Server:  %s",
		"id.link":      "Pairing link (open it in a browser): %s",
		"id.config":    "Config:  %s",
		"peers.none":   "No paired devices. Pair one with `fileswap sync <code>` or from the web app.",
		"allow.added":  "Pairing requests from %s will be accepted automatically.",
		"allow.exists": "%s is already allowed.",
	},
	"es": {
		"usage": `fileswap — envía archivos entre navegadores y servidores Linux, de igual a igual

Uso:
  fileswap [--config <dir>] <comando> [opciones]

Comandos:
  init     --name <nombre> [--server <url>]  Crea la identidad de este nodo
  id                                         Muestra el nombre, el código y el enlace de vinculación
  peers                                      Lista los dispositivos vinculados
  sync     <código>                          Pide a un dispositivo vincularse con este nodo    (alias: -sync) *
  tfr      <nodo> <ruta>                     Envía un archivo a un dispositivo vinculado       (alias: -tfr, send) *
  listen   [--dir <ruta>]                    Se queda en línea para recibir archivos y solicitudes *
  allow    <código>                          Acepta la vinculación de ese dispositivo sin preguntar
  unpair   <nodo>                            Desvincula un dispositivo *
  version                                    Muestra la versión
  help     [comando]                         Muestra esta ayuda, o la de un comando            (alias: --help, -h)

<nodo> es un código de dispositivo, un prefijo único de él, o el nombre del dispositivo.
<código> es el código completo del dispositivo (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).

Opciones:
  --config <dir>   Directorio de configuración (por defecto: $FILESWAP_CONFIG, si no $XDG_CONFIG_HOME/fileswap, si no ~/.config/fileswap)

* Todavía no disponible en esta versión.
`,
		"help.init": `Uso: fileswap init --name <nombre> [--server <url>] [--force]

Crea la identidad de este nodo: un secreto privado guardado en el directorio de
configuración y el código público que se deriva de él. Si vuelves a ejecutar
init, el código se mantiene y solo cambian el nombre o el servidor; --force crea
un código nuevo (los dispositivos vinculados tendrán que vincularse de nuevo).

  --name     Nombre que verán los otros dispositivos (obligatorio la primera vez)
  --server   Servidor FileSwap, p. ej. ws://192.168.1.10:7341 o https://fileswap.ejemplo.com
             (por defecto: ws://localhost:7341)
  --force    Reemplaza la identidad por una nueva
`,
		"help.id":      "Uso: fileswap id\n\nMuestra el nombre, el código del dispositivo, el servidor y el enlace para vincular un navegador.\n",
		"help.peers":   "Uso: fileswap peers\n\nLista los dispositivos vinculados con este nodo.\n",
		"help.sync":    "Uso: fileswap sync <código>\n\nEnvía una solicitud de vinculación al dispositivo con ese código y espera a que la acepte.\nEl otro dispositivo debe estar en línea.\n",
		"help.tfr":     "Uso: fileswap tfr <nodo> <ruta>\n\nEnvía un archivo a un dispositivo vinculado. El otro dispositivo debe estar en línea.\n",
		"help.listen":  "Uso: fileswap listen [--dir <ruta>]\n\nSe queda en línea: recibe archivos en --dir (por defecto ~/FileSwap) y atiende solicitudes de vinculación.\n",
		"help.allow":   "Uso: fileswap allow <código>\n\n`listen` aceptará sin preguntar las solicitudes de vinculación de ese código.\n",
		"help.unpair":  "Uso: fileswap unpair <nodo>\n\nDesvincula un dispositivo en ambos lados.\n",
		"help.version": "Uso: fileswap version\n",
		"help.help":    "Uso: fileswap help [comando]\n",

		"err.unknownCommand": "comando desconocido %q (ejecuta `fileswap --help`)",
		"err.notInit":        "este nodo aún no tiene identidad; ejecuta: fileswap init --name <nombre>",
		"err.nameRequired":   "--name es obligatorio la primera vez",
		"err.badCode":        "%q no es un código de dispositivo válido (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)",
		"err.badServer":      "URL de servidor no válida %q",
		"err.args":           "argumentos incorrectos",
		"err.notYet":         "`%s` todavía no está disponible en esta versión",

		"init.created": "Identidad creada.",
		"init.updated": "Identidad actualizada (el código no cambia).",
		"init.phi":     "Aviso: no uses FileSwap con datos de pacientes ni información médica (PHI); las transferencias aún no tienen cifrado de extremo a extremo.",
		"id.name":      "Nombre:   %s",
		"id.code":      "Código:   %s",
		"id.server":    "Servidor: %s",
		"id.link":      "Enlace de vinculación (ábrelo en un navegador): %s",
		"id.config":    "Config:   %s",
		"peers.none":   "No hay dispositivos vinculados. Vincula uno con `fileswap sync <código>` o desde la app web.",
		"allow.added":  "Las solicitudes de vinculación de %s se aceptarán automáticamente.",
		"allow.exists": "%s ya estaba permitido.",
	},
}
