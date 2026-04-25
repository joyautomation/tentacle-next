package sparkplug

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
