package sparkplug

import "encoding/json"

// FrameEvent is published by sparkplug-host on the bus subject SubjectHostFrame
// once per Sparkplug B frame received from the broker. Downstream consumers
// (fleet inventory, alerting, audit trails) subscribe instead of opening their
// own MQTT connection.
type FrameEvent struct {
	Type        string `json:"type"`        // NBIRTH, NDEATH, DBIRTH, DDEATH, NDATA, DDATA
	GroupID     string `json:"groupId"`
	EdgeNode    string `json:"edgeNode"`
	Device      string `json:"device,omitempty"` // empty for node-level frames
	Timestamp   int64  `json:"timestamp"`        // unix ms when host received it
	MetricCount int    `json:"metricCount"`
	BdSeq       int64  `json:"bdSeq,omitempty"` // present on NBIRTH (and NDEATH per spec)
}

// SubjectHostFrame is the bus subject sparkplug-host publishes FrameEvent values to.
const SubjectHostFrame = "sparkplug.host.frame"

// SubjectHostNodes is the bus subject sparkplug-host serves node inventory snapshots on.
// Reply with a JSON array of Node entries.
const SubjectHostNodes = "sparkplug.host.nodes"

// SubjectHostNodesDelete is the bus subject sparkplug-host accepts inventory eviction
// requests on. Request payload: {"groupId":"...","nodeId":"..."}. Reply: {"removed":bool}.
// Note: if the edge keeps publishing NBIRTH it will reappear; pair with a gitops repo
// delete to fully evict the node.
const SubjectHostNodesDelete = "sparkplug.host.nodes.delete"

// SubjectHostVerb is the bus subject sparkplug-host accepts NCMD verb RPC
// invocations on. Request: HostVerbRequest. Reply: RPCResponse JSON
// (or {"error":"..."} envelope when the host failed before publishing NCMD).
const SubjectHostVerb = "sparkplug.host.verb"

// SubjectHostBrowseCache is the bus subject sparkplug-host serves per-device
// browse caches on (the cache lives in Node.BrowseCaches and is captured
// from edge "_meta/browse" metrics). Request: HostBrowseCacheRequest. Reply:
// HostBrowseCacheReply.
const SubjectHostBrowseCache = "sparkplug.host.browse_cache"

// HostVerbRequest is the bus payload for SubjectHostVerb. Params is the
// verb-specific JSON payload mantle ships in the NCMD body.
type HostVerbRequest struct {
	GroupID   string          `json:"groupId"`
	NodeID    string          `json:"nodeId"`
	Verb      string          `json:"verb"`
	Params    json.RawMessage `json:"params,omitempty"`
	TimeoutMs int             `json:"timeoutMs,omitempty"`
}

// HostBrowseCacheRequest is the bus payload for SubjectHostBrowseCache.
type HostBrowseCacheRequest struct {
	GroupID  string `json:"groupId"`
	NodeID   string `json:"nodeId"`
	DeviceID string `json:"deviceId"`
}

// HostBrowseCacheReply is the bus reply for SubjectHostBrowseCache. Cache is
// the same JSON the local edge browse-cache API serves (or null if missing).
type HostBrowseCacheReply struct {
	Cache json.RawMessage `json:"cache,omitempty"`
	Error string          `json:"error,omitempty"`
}

// Reserved Sparkplug metric namespace for edge↔mantle interop signals.
// Anything under this prefix bypasses the historian on mantle and gets
// routed into per-node observed-state instead. Keep in sync with
// sparkplughost.metaPrefix.
const MetaPrefix = "_meta/"

// Verb metric names — Sparkplug B convention: Node Control/<Verb> for the
// command (NCMD), Node Status/<Verb> for the reply (NDATA). Both sides ship
// a JSON-encoded RPC envelope as the metric's String value.
const (
	VerbBrowse           = "Browse"
	NodeControlPrefix    = "Node Control/"
	NodeStatusPrefix     = "Node Status/"
	MetricNodeCtlBrowse  = NodeControlPrefix + VerbBrowse
	MetricNodeStatBrowse = NodeStatusPrefix + VerbBrowse
)

// RPCRequest is the JSON envelope for an NCMD verb invocation. Carried as
// the String value of a Node Control/<Verb> metric. Params is verb-specific.
type RPCRequest struct {
	RequestID string          `json:"requestId"`
	Verb      string          `json:"verb"`
	Params    json.RawMessage `json:"params,omitempty"`
}

// RPCResponse is the JSON envelope for the matching NDATA reply on
// Node Status/<Verb>. OK==false means the edge rejected/failed the request
// and Error explains why; Result is verb-specific success payload.
type RPCResponse struct {
	RequestID string          `json:"requestId"`
	Verb      string          `json:"verb"`
	OK        bool            `json:"ok"`
	Error     string          `json:"error,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// BrowseRequestParams is the Params payload for a Browse verb call. Mirrors
// the JSON body of the local POST /api/v1/gateways/{id}/browse so the edge
// can pass it straight through to its scanner subject.
type BrowseRequestParams struct {
	GatewayID string          `json:"gatewayId"`
	DeviceID  string          `json:"deviceId"`
	Protocol  string          `json:"protocol"`
	Input     json.RawMessage `json:"input"`
}

// BrowseRequestResult is the success payload for a Browse verb reply: edge
// accepted the request and started the browse. The actual tag list lands
// later as a "_meta/browse" DDATA on the device.
type BrowseRequestResult struct {
	BrowseID string `json:"browseId"`
}
