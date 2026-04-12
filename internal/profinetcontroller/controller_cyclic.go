//go:build profinetcontroller || all

package profinetcontroller

import (
	"context"
	"encoding/binary"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joyautomation/tentacle/internal/profinet"
)

// ControllerCyclic manages cyclic RT data exchange from the controller perspective.
// The controller:
// - Sends output data TO the device (using OutputFrameID)
// - Receives input data FROM the device (matching InputFrameID)
type ControllerCyclic struct {
	transport *profinet.Transport
	ar        *ControllerAR
	log       *slog.Logger

	// Output data (controller → device)
	outputData []byte
	outputIOCS []byte
	outputMu   sync.Mutex

	// Input callback
	onInputData func(data []byte)

	cycleCounter uint16
	running      atomic.Bool
	cancel       context.CancelFunc

	// Watchdog
	lastInputRx time.Time
	watchdogMs  uint32
	inputMu     sync.Mutex
}

// NewControllerCyclic creates a new controller-side cyclic handler.
func NewControllerCyclic(transport *profinet.Transport, ar *ControllerAR, onInputData func(data []byte), log *slog.Logger) *ControllerCyclic {
	outputLen := int(ar.OutputDataLen)
	if outputLen == 0 {
		outputLen = 1 // minimum 1 byte for IOCS
	}

	watchdogMs := uint32(ar.CycleTimeMs) * 10
	if watchdogMs < 100 {
		watchdogMs = 100
	}

	return &ControllerCyclic{
		transport:   transport,
		ar:          ar,
		log:         log,
		outputData:  make([]byte, outputLen),
		outputIOCS:  []byte{profinet.IOxSGood},
		onInputData: onInputData,
		watchdogMs:  watchdogMs,
		lastInputRx: time.Now(),
	}
}

// Start begins the cyclic output sender. Blocks until stopped.
// Input frames are dispatched via HandleInputFrame by the scanner's frame loop.
func (cc *ControllerCyclic) Start(ctx context.Context) {
	ctx, cc.cancel = context.WithCancel(ctx)
	cc.running.Store(true)
	defer cc.running.Store(false)

	cyclePeriod := time.Duration(cc.ar.CycleTimeMs) * time.Millisecond
	if cyclePeriod < time.Millisecond {
		cyclePeriod = time.Millisecond
	}

	cc.log.Info("controller-cyclic: started",
		"period", cyclePeriod,
		"outputLen", len(cc.outputData),
		"outputFrameID", cc.ar.OutputFrameID,
		"inputFrameID", cc.ar.InputFrameID,
	)

	ticker := time.NewTicker(cyclePeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cc.sendOutputFrame()
		}
	}
}

// Stop terminates cyclic exchange.
func (cc *ControllerCyclic) Stop() {
	if cc.cancel != nil {
		cc.cancel()
	}
}

// IsRunning returns true if cyclic exchange is active.
func (cc *ControllerCyclic) IsRunning() bool {
	return cc.running.Load()
}

// SetOutputData updates the output buffer for the next cyclic frame.
func (cc *ControllerCyclic) SetOutputData(data []byte) {
	cc.outputMu.Lock()
	defer cc.outputMu.Unlock()
	copy(cc.outputData, data)
}

// WriteOutputTag writes a single value into the output buffer at the given offset.
func (cc *ControllerCyclic) WriteOutputTag(offset uint16, data []byte) {
	cc.outputMu.Lock()
	defer cc.outputMu.Unlock()
	end := int(offset) + len(data)
	if end <= len(cc.outputData) {
		copy(cc.outputData[offset:end], data)
	}
}

// sendOutputFrame sends one cyclic output RT frame to the device.
func (cc *ControllerCyclic) sendOutputFrame() {
	if cc.ar.OutputFrameID == 0 {
		return
	}

	cc.outputMu.Lock()

	// Build RT frame: FrameID(2) + Data + IOCS + CycleCounter(2) + DataStatus(1) + TransferStatus(1)
	frameLen := 2 + len(cc.outputData) + len(cc.outputIOCS) + 4
	frame := make([]byte, frameLen)

	binary.BigEndian.PutUint16(frame[0:2], cc.ar.OutputFrameID)
	copy(frame[2:], cc.outputData)
	copy(frame[2+len(cc.outputData):], cc.outputIOCS)

	trailerOffset := 2 + len(cc.outputData) + len(cc.outputIOCS)
	binary.BigEndian.PutUint16(frame[trailerOffset:], cc.cycleCounter)
	frame[trailerOffset+2] = profinet.DataStatusNormal
	frame[trailerOffset+3] = 0x00

	cc.cycleCounter++
	cc.outputMu.Unlock()

	if err := cc.transport.SendFrame(cc.ar.DeviceMAC, frame); err != nil {
		cc.log.Debug("controller-cyclic: send output failed", "error", err)
	}
}

// HandleInputFrame processes an incoming cyclic input RT frame from the device.
// Called by the scanner's central frame dispatcher.
func (cc *ControllerCyclic) HandleInputFrame(payload []byte) {
	if len(payload) < 2 {
		return
	}

	frameID := binary.BigEndian.Uint16(payload[0:2])
	if frameID != cc.ar.InputFrameID {
		return
	}

	cc.inputMu.Lock()
	cc.lastInputRx = time.Now()
	cc.inputMu.Unlock()

	// Strip FrameID, pass data to handler
	data := payload[2:]
	if cc.onInputData != nil {
		cc.onInputData(data)
	}
}

// WatchdogExpired returns true if no input data has been received within the watchdog period.
func (cc *ControllerCyclic) WatchdogExpired() bool {
	cc.inputMu.Lock()
	defer cc.inputMu.Unlock()
	return time.Since(cc.lastInputRx) > time.Duration(cc.watchdogMs)*time.Millisecond
}

// InputFrameID returns the expected input FrameID for frame routing.
func (cc *ControllerCyclic) InputFrameID() uint16 {
	return cc.ar.InputFrameID
}
