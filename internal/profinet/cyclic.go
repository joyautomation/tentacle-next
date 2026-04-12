//go:build profinet || profinetall

package profinet

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// CyclicHandler manages RT cyclic data exchange (PPM and CPM).
// PPM = Provider Protocol Machine: sends input data (device → controller).
// CPM = Consumer Protocol Machine: receives output data (controller → device).
type CyclicHandler struct {
	transport *Transport
	ar        *AR
	cfg       *ProfinetConfig
	log       *slog.Logger

	// Input data (device → controller)
	inputData []byte
	inputIOPS []byte // one IOPS byte per input submodule

	// Output data (controller → device)
	outputData []byte
	outputIOCS []byte // one IOCS byte per output submodule

	// Callbacks for data integration
	getInputData func(sub *SubslotConfig) []byte
	onOutputData func(sub *SubslotConfig, data []byte)

	cycleCounter uint16

	mu      sync.Mutex
	cancel  context.CancelFunc
	running atomic.Bool

	// Watchdog
	lastOutputRx time.Time
	watchdogMs   uint32
}

// CyclicCallbacks provides data integration hooks.
type CyclicCallbacks struct {
	GetInputData func(sub *SubslotConfig) []byte
	OnOutputData func(sub *SubslotConfig, data []byte)
}

// NewCyclicHandler creates a new cyclic data handler for an AR.
func NewCyclicHandler(transport *Transport, ar *AR, cfg *ProfinetConfig, callbacks CyclicCallbacks, log *slog.Logger) *CyclicHandler {
	// Calculate watchdog timeout
	var watchdogMs uint32
	if ar.OutputIOCR != nil {
		cyclePeriodUs := uint32(ar.OutputIOCR.SendClockFactor) *
			uint32(ar.OutputIOCR.ReductionRatio) * 31250 / 1000 // base is 31.25µs
		watchdogMs = cyclePeriodUs * uint32(ar.OutputIOCR.WatchdogFactor) / 1000
		if watchdogMs < 100 {
			watchdogMs = 100
		}
	}

	var inputSize, outputSize int
	if ar.InputIOCR != nil {
		inputSize = int(ar.InputIOCR.DataLength)
	}
	if ar.OutputIOCR != nil {
		outputSize = int(ar.OutputIOCR.DataLength)
	}

	return &CyclicHandler{
		transport:    transport,
		ar:           ar,
		cfg:          cfg,
		log:          log,
		inputData:    make([]byte, inputSize),
		inputIOPS:    make([]byte, countSubmodules(cfg, true)),
		outputData:   make([]byte, outputSize),
		outputIOCS:   make([]byte, countSubmodules(cfg, false)),
		getInputData: callbacks.GetInputData,
		onOutputData: callbacks.OnOutputData,
		watchdogMs:   watchdogMs,
		lastOutputRx: time.Now(),
	}
}

// countSubmodules counts submodules with the given direction.
func countSubmodules(cfg *ProfinetConfig, input bool) int {
	count := 0
	for _, slot := range cfg.Slots {
		for _, sub := range slot.Subslots {
			if input && (sub.Direction == DirectionInput || sub.Direction == DirectionInputOutput) {
				count++
			}
			if !input && (sub.Direction == DirectionOutput || sub.Direction == DirectionInputOutput) {
				count++
			}
		}
	}
	if count == 0 {
		count = 1 // At least one IOPS/IOCS byte
	}
	return count
}

// Start begins PPM cyclic sending. Blocks until stopped or context cancelled.
// Output frames are dispatched by the device frame loop via HandleOutputFrame.
func (h *CyclicHandler) Start(ctx context.Context) {
	ctx, h.cancel = context.WithCancel(ctx)
	h.running.Store(true)
	defer h.running.Store(false)

	// Calculate cycle time from IOCR parameters
	var cyclePeriod time.Duration
	if h.ar.InputIOCR != nil {
		// SendClockFactor * ReductionRatio * 31.25µs
		periodNs := int64(h.ar.InputIOCR.SendClockFactor) *
			int64(h.ar.InputIOCR.ReductionRatio) * 31250
		cyclePeriod = time.Duration(periodNs) * time.Nanosecond
	} else {
		cyclePeriod = time.Millisecond
	}

	h.log.Info("cyclic: started",
		"period", cyclePeriod,
		"inputLen", len(h.inputData),
		"outputLen", len(h.outputData),
	)

	ticker := time.NewTicker(cyclePeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.sendInputFrame()
		}
	}
}

// Stop terminates cyclic exchange.
func (h *CyclicHandler) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
}

// IsRunning returns true if cyclic exchange is active.
func (h *CyclicHandler) IsRunning() bool {
	return h.running.Load()
}

// sendInputFrame packs and sends one RT input frame (device → controller).
func (h *CyclicHandler) sendInputFrame() {
	if h.ar.InputIOCR == nil {
		return
	}

	h.mu.Lock()

	// Collect input data from all input submodules
	offset := 0
	iopsIdx := 0
	for _, slot := range h.cfg.Slots {
		for _, sub := range slot.Subslots {
			if sub.Direction != DirectionInput && sub.Direction != DirectionInputOutput {
				continue
			}

			if h.getInputData != nil {
				data := h.getInputData(&sub)
				if len(data) > 0 && offset+len(data) <= len(h.inputData) {
					copy(h.inputData[offset:], data)
				}
			}
			offset += int(sub.InputSize)

			if iopsIdx < len(h.inputIOPS) {
				h.inputIOPS[iopsIdx] = IOxSGood
			}
			iopsIdx++
		}
	}

	// Build RT frame:
	// FrameID(2) + Data + IOPS + CycleCounter(2) + DataStatus(1) + TransferStatus(1)
	frameLen := 2 + len(h.inputData) + len(h.inputIOPS) + 4
	frame := make([]byte, frameLen)

	binary.BigEndian.PutUint16(frame[0:2], h.ar.InputIOCR.FrameID)
	copy(frame[2:], h.inputData)
	copy(frame[2+len(h.inputData):], h.inputIOPS)

	trailerOffset := 2 + len(h.inputData) + len(h.inputIOPS)
	binary.BigEndian.PutUint16(frame[trailerOffset:], h.cycleCounter)
	frame[trailerOffset+2] = DataStatusNormal
	frame[trailerOffset+3] = 0x00 // TransferStatus OK

	h.cycleCounter++
	h.mu.Unlock()

	dstMAC := h.ar.InitiatorMAC
	if err := h.transport.SendFrame(dstMAC, frame); err != nil {
		h.log.Debug("cyclic: send input frame failed", "error", err)
	}
}

// HandleOutputFrame processes a received output RT frame from the controller.
// Called by the device's central frame dispatcher.
func (h *CyclicHandler) HandleOutputFrame(payload []byte) {
	if len(payload) < 2 {
		return
	}

	frameID := binary.BigEndian.Uint16(payload[0:2])
	if h.ar.OutputIOCR == nil || frameID != h.ar.OutputIOCR.FrameID {
		return
	}

	h.processOutputData(payload[2:])
}

func (h *CyclicHandler) processOutputData(data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.lastOutputRx = time.Now()

	dataLen := len(h.outputData)
	if dataLen > len(data) {
		dataLen = len(data)
	}
	copy(h.outputData[:dataLen], data[:dataLen])

	// Dispatch to submodule handlers
	offset := 0
	for _, slot := range h.cfg.Slots {
		for _, sub := range slot.Subslots {
			if sub.Direction != DirectionOutput && sub.Direction != DirectionInputOutput {
				continue
			}

			end := offset + int(sub.OutputSize)
			if end > len(h.outputData) {
				break
			}

			if h.onOutputData != nil {
				subData := make([]byte, sub.OutputSize)
				copy(subData, h.outputData[offset:end])
				h.onOutputData(&sub, subData)
			}
			offset = end
		}
	}
}

// WatchdogExpired returns true if the output watchdog has expired.
func (h *CyclicHandler) WatchdogExpired() bool {
	if h.watchdogMs == 0 {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return time.Since(h.lastOutputRx) > time.Duration(h.watchdogMs)*time.Millisecond
}

// IsRTCyclicFrame checks if the FrameID indicates an RT cyclic data frame.
func IsRTCyclicFrame(frameID uint16) bool {
	return frameID >= 0x0100 && frameID <= 0xF7FF
}

// BuildRTFrame constructs a minimal RT cyclic frame for testing.
func BuildRTFrame(frameID uint16, data []byte, iocs []byte, cycleCounter uint16) []byte {
	frameLen := 2 + len(data) + len(iocs) + 4
	frame := make([]byte, frameLen)
	binary.BigEndian.PutUint16(frame[0:2], frameID)
	copy(frame[2:], data)
	copy(frame[2+len(data):], iocs)
	trailerOffset := 2 + len(data) + len(iocs)
	binary.BigEndian.PutUint16(frame[trailerOffset:], cycleCounter)
	frame[trailerOffset+2] = DataStatusNormal
	frame[trailerOffset+3] = 0x00
	return frame
}

// FrameID for checking if output belongs to a specific IOCR.
func (h *CyclicHandler) OutputFrameID() uint16 {
	if h.ar.OutputIOCR != nil {
		return h.ar.OutputIOCR.FrameID
	}
	return 0
}

// InputFrameID returns the device-assigned input frame ID.
func (h *CyclicHandler) InputFrameID() uint16 {
	if h.ar.InputIOCR != nil {
		return h.ar.InputIOCR.FrameID
	}
	return 0
}

// SetDstMAC overrides the destination MAC for testing with loopback/veth.
func (h *CyclicHandler) SetDstMAC(mac net.HardwareAddr) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ar.InitiatorMAC = mac
}
