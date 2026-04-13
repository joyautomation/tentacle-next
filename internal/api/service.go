//go:build api || all

package api

import (
	"net/http"
	"os"
	"time"

	"github.com/joyautomation/tentacle/internal/service"
)

func (m *Module) handleGetServiceStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, service.GetStatus(m.mode))
}

func (m *Module) handleServiceInstall(w http.ResponseWriter, _ *http.Request) {
	if err := service.Install(m.log); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Service installed and enabled. Click Activate to start it.",
	})
}

func (m *Module) handleServiceActivate(w http.ResponseWriter, _ *http.Request) {
	if err := service.Activate(m.log); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"message":    "Service starting. This process will exit shortly.",
		"activating": true,
	})

	// Give the HTTP response time to flush, then exit so the service can bind the port.
	go func() {
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}()
}

func (m *Module) handleServiceUninstall(w http.ResponseWriter, _ *http.Request) {
	if err := service.Uninstall(false, m.log); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Service uninstalled.",
	})
}
