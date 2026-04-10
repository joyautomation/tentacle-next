//go:build api || all

// Package api implements the REST API module replacing tentacle-graphql.
// Uses chi router with SSE for real-time subscriptions.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/web"
	ttypes "github.com/joyautomation/tentacle/types"
)

const busTimeout = 10 * time.Second

// Module implements the REST API.
type Module struct {
	moduleID string
	port     int
	bus      bus.Bus
	server   *http.Server
	cancel   context.CancelFunc
	log      *slog.Logger
	mode     string

	// Service log ring buffer for GET queries.
	logsMu sync.RWMutex
	logBuf []ttypes.ServiceLogEntry
	logSub bus.Subscription

	// Browse state tracking (in-memory).
	browseMu     sync.RWMutex
	browseCache  map[string]json.RawMessage // "gatewayId:deviceId" → result
	browseStates map[string]*BrowseState    // browseId → state

	// NATS traffic collector.
	traffic *trafficCollector
}

// BrowseState tracks an in-progress or completed browse operation.
type BrowseState struct {
	BrowseID  string          `json:"browseId"`
	GatewayID string          `json:"gatewayId,omitempty"`
	DeviceID  string          `json:"deviceId"`
	Protocol  string          `json:"protocol"`
	Status    string          `json:"status"` // "in-progress", "completed", "failed"
	StartedAt int64           `json:"startedAt"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// New creates a new API module.
func New(moduleID string) *Module {
	port := 4000
	if p := os.Getenv("API_PORT"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			port = v
		}
	}
	mode := os.Getenv("TENTACLE_MODE")
	if mode == "" {
		// Auto-detect deployment mode
		if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
			mode = "kubernetes"
		} else if _, err := os.Stat("/.dockerenv"); err == nil {
			mode = "docker"
		} else if os.Getppid() == 1 || os.Getenv("INVOCATION_ID") != "" {
			mode = "systemd"
		} else {
			mode = "dev"
		}
	}
	return &Module{
		moduleID:     moduleID,
		port:         port,
		log:          slog.Default(),
		logBuf:       make([]ttypes.ServiceLogEntry, 0, 1000),
		mode:         mode,
		browseCache:  make(map[string]json.RawMessage),
		browseStates: make(map[string]*BrowseState),
		traffic:      newTrafficCollector(),
	}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return "api" }

func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.bus = b
	m.log = slog.Default().With("serviceType", m.ServiceType(), "moduleID", m.ModuleID())
	ctx, m.cancel = context.WithCancel(ctx)

	stopHB := heartbeat.Start(b, m.moduleID, m.ServiceType(), nil)
	defer stopHB()

	// Start NATS traffic collector.
	if err := m.traffic.start(b); err != nil {
		m.log.Warn("failed to start traffic collector", "error", err)
	}

	// Buffer service logs for the GET endpoint.
	if sub, err := b.Subscribe("service.logs.>", func(_ string, data []byte, _ bus.ReplyFunc) {
		var entry ttypes.ServiceLogEntry
		if json.Unmarshal(data, &entry) != nil {
			return
		}
		m.logsMu.Lock()
		if len(m.logBuf) >= 1000 {
			m.logBuf = m.logBuf[1:]
		}
		m.logBuf = append(m.logBuf, entry)
		m.logsMu.Unlock()
	}); err == nil {
		m.logSub = sub
	}

	m.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", m.port),
		Handler: m.routes(),
	}
	ln, err := net.Listen("tcp", m.server.Addr)
	if err != nil {
		return fmt.Errorf("api: listen %s: %w", m.server.Addr, err)
	}
	m.log.Info("API server started", "port", m.port)

	errCh := make(chan error, 1)
	go func() { errCh <- m.server.Serve(ln) }()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	}
	return nil
}

func (m *Module) Stop() error {
	m.traffic.stop()
	if m.logSub != nil {
		m.logSub.Unsubscribe()
	}
	if m.cancel != nil {
		m.cancel()
	}
	if m.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return m.server.Shutdown(ctx)
	}
	return nil
}

func (m *Module) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(corsMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/mode", m.handleGetMode)

		// Variables
		r.Get("/variables", m.handleListVariables)
		r.Get("/variables/stream", m.handleStreamVariables)
		r.Get("/variables/stream/batch", m.handleStreamVariableBatch)
		r.Get("/variables/{variableId}", m.handleGetVariable)
		r.Get("/variables/{variableId}/stream", m.handleStreamVariable)
		r.Put("/variables/{moduleId}/{variableId}/value", m.handleWriteVariable)

		// Services
		r.Get("/services", m.handleListServices)
		r.Put("/services/{moduleId}/enabled", m.handleSetServiceEnabled)
		r.Post("/services/{moduleId}/restart", m.handleRestartService)
		r.Get("/services/{serviceType}/logs", m.handleGetServiceLogs)
		r.Get("/services/{serviceType}/logs/stream", m.handleStreamServiceLogs)

		// Gateways
		r.Get("/gateways", m.handleListGateways)
		r.Get("/gateways/{gatewayId}", m.handleGetGateway)
		r.Put("/gateways/{gatewayId}/devices", m.handleSetGatewayDevice)
		r.Delete("/gateways/{gatewayId}/devices/{deviceId}", m.handleDeleteGatewayDevice)
		r.Put("/gateways/{gatewayId}/devices/{deviceId}/template-overrides", m.handleSetTemplateOverrides)
		r.Put("/gateways/{gatewayId}/variables", m.handleSetGatewayVariables)
		r.Put("/gateways/{gatewayId}/variables/{variableId}", m.handleSetGatewayVariable)
		r.Delete("/gateways/{gatewayId}/variables/{variableId}", m.handleDeleteGatewayVariable)
		r.Post("/gateways/{gatewayId}/variables/delete", m.handleDeleteGatewayVariables)
		r.Delete("/gateways/{gatewayId}/udt-variables/{udtVariableId}", m.handleDeleteGatewayUdtVariable)
		r.Post("/gateways/{gatewayId}/udt-variables/delete", m.handleDeleteGatewayUdtVariables)
		r.Post("/gateways/{gatewayId}/devices/{deviceId}/sync", m.handleSyncGatewayDeviceVariables)
		r.Post("/gateways/{gatewayId}/import-browse", m.handleImportGatewayBrowse)
		r.Put("/gateways/{gatewayId}/udt-config/{templateName}", m.handleUpdateGatewayUdtConfig)
		r.Get("/gateways/{gatewayId}/browse-cache/{deviceId}", m.handleGetGatewayBrowseCache)
		r.Get("/gateways/browse-states", m.handleGetGatewayBrowseStates)
		r.Get("/gateways/{gatewayId}/browse-state/{deviceId}", m.handleGetGatewayBrowseState)

		// Scanner / Browse
		r.Post("/browse/{protocol}", m.handleBrowseTags)
		r.Get("/browse/{browseId}/progress", m.handleStreamBrowseProgress)
		r.Post("/gateways/{gatewayId}/browse", m.handleStartGatewayBrowse)
		r.Get("/gateways/{gatewayId}/browse/{browseId}/progress", m.handleStreamGatewayBrowseProgress)
		r.Post("/scanner/{protocol}/subscribe", m.handleScannerSubscribe)
		r.Post("/scanner/{protocol}/unsubscribe", m.handleScannerUnsubscribe)

		// Network
		r.Get("/network/interfaces", m.handleGetNetworkInterfaces)
		r.Get("/network/config", m.handleGetNetworkConfig)
		r.Put("/network/config", m.handleApplyNetworkConfig)
		r.Get("/network/stream", m.handleStreamNetworkState)

		// Nftables
		r.Get("/nftables/config", m.handleGetNftablesConfig)
		r.Put("/nftables/config", m.handleApplyNftablesConfig)
		r.Get("/nftables/stream", m.handleStreamNftablesConfig)

		// NATS Traffic
		r.Get("/nats/traffic", m.handleGetNatsTraffic)
		r.Get("/nats/traffic/stream", m.handleStreamNatsTraffic)

		// MQTT
		r.Get("/mqtt/metrics", m.handleGetMqttMetrics)
		r.Get("/mqtt/metrics/stream", m.handleStreamMqttMetrics)
		r.Get("/mqtt/store-forward", m.handleGetStoreForwardStatus)

		// History
		r.Get("/history", m.handleQueryHistory)
		r.Get("/history/usage", m.handleGetHistoryUsage)
		r.Get("/history/enabled", m.handleGetHistoryEnabled)

		// GitOps setup
		r.Get("/gitops/ssh-key", m.handleGetSSHKey)
		r.Post("/gitops/ssh-key/generate", m.handleGenerateSSHKey)
		r.Post("/gitops/test-connection", m.handleTestGitConnection)

		// System
		r.Get("/system/hostname", m.handleGetHostname)

		// Config
		r.Get("/config", m.handleGetAllConfig)
		r.Get("/config/{moduleId}/schema", m.handleGetConfigSchema)
		r.Get("/config/{moduleId}", m.handleGetServiceConfig)
		r.Put("/config/{moduleId}/{envVar}", m.handleUpdateServiceConfig)

		// Manifest management
		r.Get("/export", m.handleExport)
		r.Post("/apply", m.handleApply)
		r.Post("/validate", m.handleValidate)
		r.Post("/diff", m.handleDiff)

		// Orchestrator
		r.Get("/orchestrator/desired-services", m.handleListDesiredServices)
		r.Put("/orchestrator/desired-services/{moduleId}", m.handleSetDesiredService)
		r.Delete("/orchestrator/desired-services/{moduleId}", m.handleDeleteDesiredService)
		r.Get("/orchestrator/service-statuses", m.handleListServiceStatuses)
		r.Get("/orchestrator/modules", m.handleListModules)
		r.Get("/orchestrator/modules/{moduleId}/versions", m.handleGetModuleVersions)
		r.Get("/orchestrator/internet", m.handleCheckInternet)
	})

	// Serve embedded web UI (SPA with fallback to index.html).
	r.Handle("/*", web.Handler())

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Module) handleGetMode(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"mode": m.mode})
}

func newRequestID() string { return uuid.New().String() }
