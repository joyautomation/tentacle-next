//go:build (api || all) && (plc || all)

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/joyautomation/tentacle/internal/plc/lsp"
)

// wsUpgrader handles the plain WebSocket upgrade for the LSP channel. We
// accept any origin because the editor is served from the same origin as
// the API in every deployment topology; if that stops being true we tighten
// this to an explicit allow-list.
var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handlePlcLSP upgrades the request to a WebSocket and runs a one-session
// LSP server against it. One server per connection; document state lives
// for the life of the socket.
func (m *Module) handlePlcLSP(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an error response; just bail.
		return
	}
	tr := &wsTransport{conn: conn}
	plcID := chi.URLParam(r, "plcId")
	server := lsp.NewServer(m.log, &plcLspProvider{mod: m, plcID: plcID})
	// Serve blocks until the peer disconnects or the server errors.
	if err := server.Serve(r.Context(), tr); err != nil {
		m.log.Warn("plc lsp session ended with error", "err", err)
	}
}

// wsTransport adapts a gorilla WebSocket to lsp.Transport. One JSON-RPC
// message per WS frame; we use TextMessage since the payload is UTF-8 JSON.
type wsTransport struct {
	conn *websocket.Conn
}

func (t *wsTransport) ReadMessage() ([]byte, error) {
	_, data, err := t.conn.ReadMessage()
	return data, err
}

func (t *wsTransport) WriteMessage(data []byte) error {
	return t.conn.WriteMessage(websocket.TextMessage, data)
}

func (t *wsTransport) Close() error {
	return t.conn.Close()
}
