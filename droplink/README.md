# DropLink

Transferencia anónima de archivos entre dispositivos. WebRTC P2P, cifrado en tránsito, el servidor solo intercambia señales de emparejamiento — nunca ve el contenido.

## Características

- **Sin cuenta** — solo un nombre de dispositivo, guardado en `localStorage`
- **QR pairing** — el escritorio muestra un QR; el móvil lo escanea para vincularse
- **Sesión persistente** — los pares se guardan en `localStorage`; la próxima vez no necesitas re-vincular
- **Presencia en tiempo real** — indicador verde/rojo según si el dispositivo está activo en ese momento
- **Transferencia P2P** — los archivos van directo de un navegador al otro (WebRTC DataChannel)
- **Link de descarga** — el receptor obtiene un botón de descarga en su pantalla
- **Responsive** — funciona en móvil y escritorio

---

## Uso local (recomendado para desarrollo)

### Requisitos
- Node.js 18+

### Pasos

```bash
cd droplink/server
npm install
npm start
```

Abre **http://localhost:3000** en tu computador y también en tu teléfono (en la misma red WiFi).

> **Tip:** para que el teléfono acceda a tu computador local, usa la IP de tu máquina en lugar de `localhost`.  
> Ejemplo: `http://192.168.1.X:3000`  
> Puedes encontrar tu IP con `ipconfig` (Windows) o `ifconfig` / `ip a` (Mac/Linux).

---

## Deploy en Railway / Render / Fly.io

El servidor es stateless (todo en memoria) y arranca con un solo proceso. Cualquier plataforma que soporte Node.js funciona.

### Railway

```bash
railway init
railway up
```

### Render

1. Nuevo servicio → Web Service
2. Root directory: `server`
3. Build command: `npm install`
4. Start command: `node index.js`

### Docker

```bash
# Build y run local
docker-compose up --build

# O solo Docker
docker build -f server/Dockerfile -t droplink .
docker run -p 3000:3000 droplink
```

### Variables de entorno

Copia `server/.env.example` → `server/.env` y ajusta según necesites.

| Variable | Default | Descripción |
|---|---|---|
| `PORT` | 3000 | Puerto HTTP |
| `SESSION_TTL_MS` | 1800000 | TTL de sesión (30 min) |
| `MAX_MSG_BYTES` | 16384 | Tamaño máximo de mensaje WS (16 KB) |
| `MAX_SESSIONS` | 50000 | Sesiones simultáneas en memoria |
| `RATE_LIMIT_PER_MIN` | 120 | Mensajes por minuto por dispositivo |
| `CORS_ORIGIN` | * | Origen CORS permitido |

---

## Cómo vincular dos dispositivos

1. Abre la app en ambos dispositivos
2. En el primero, pulsa **"Vincular nuevo dispositivo"** → se muestra el QR
3. En el segundo, escanea el QR con la cámara (o abre el enlace manualmente)
4. El primer dispositivo verá una solicitud → pulsa **Aceptar**
5. ¡Listo! Ambos aparecen en la lista del otro. La próxima vez que los dos estén online, se reconocen automáticamente

---

## Cómo enviar un archivo

1. Asegúrate de que el dispositivo receptor esté activo (punto verde)
2. Haz click en el dispositivo en tu lista
3. Selecciona o arrastra un archivo
4. Pulsa **Enviar**
5. El receptor verá la transferencia y recibirá un botón de **Descarga**

> Si el dispositivo aparece en gris (sin sesión activa), no es posible enviar — el archivo no tiene a dónde ir.

---

## Arquitectura

```
droplink/
  server/
    index.js          ← Servidor de señalización (WebSocket + HTTP)
    package.json
    .env.example
    Dockerfile
  client/
    index.html        ← SPA completa (HTML + CSS + JS en un solo archivo)
  docker-compose.yml
  README.md
```

El servidor actúa únicamente como intermediario para el handshake WebRTC:

```
Dispositivo A ──[SDP Offer]──▶ Servidor ──[relay]──▶ Dispositivo B
Dispositivo A ◀──[SDP Answer]── Servidor ◀──[relay]── Dispositivo B
              ──[ICE candidates ×N]──▶◀──────────────

→ Conexión P2P establecida. Servidor ya no participa en la transferencia.
```

---

## Próximos pasos (roadmap)

- [ ] Cifrado E2E con Web Crypto API (ECDH + AES-GCM)
- [ ] Múltiples archivos por sesión
- [ ] Transferencia bidireccional simultánea
- [ ] File System Access API para archivos > 2 GB
- [ ] TURN server para redes corporativas con NAT simétrico
- [ ] Drive cifrado por inodo (v2)
