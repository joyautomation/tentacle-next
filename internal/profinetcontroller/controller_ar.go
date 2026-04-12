//go:build profinetcontroller

package profinetcontroller

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"

	"github.com/joyautomation/tentacle/internal/profinet"
)

// ControllerAR manages the Application Relationship lifecycle from the
// IO Controller perspective.
type ControllerAR struct {
	ARUUID        [16]byte
	SessionKey    uint16
	DeviceMAC     net.HardwareAddr
	DeviceIP      net.IP
	LocalMAC      net.HardwareAddr
	StationName   string
	State         string
	InputFrameID  uint16 // Assigned by device (from Connect response)
	OutputFrameID uint16 // Assigned by controller
	InputDataLen  uint16
	OutputDataLen uint16
	CycleTimeMs   int
	Slots         []SlotSubscription

	rpc *RPCClient
	log *slog.Logger
}

// AR states from controller perspective.
const (
	ControllerARStateClosed    = "CLOSED"
	ControllerARStateConnected = "CONNECTED"
	ControllerARStateData      = "DATA"
)

// NewControllerAR creates a new controller-side AR.
func NewControllerAR(deviceIP net.IP, deviceMAC net.HardwareAddr, localMAC net.HardwareAddr,
	stationName string, slots []SlotSubscription, cycleTimeMs int, log *slog.Logger) (*ControllerAR, error) {

	rpc, err := NewRPCClient(deviceIP, log)
	if err != nil {
		return nil, fmt.Errorf("create RPC client: %w", err)
	}

	// Generate random AR UUID
	var arUUID [16]byte
	_, _ = rand.Read(arUUID[:])

	// Calculate total I/O data sizes
	var inputLen, outputLen uint16
	for _, slot := range slots {
		for _, sub := range slot.Subslots {
			inputLen += sub.InputSize
			outputLen += sub.OutputSize
		}
	}

	// Controller assigns output FrameID (it sends output to device)
	outputFrameID := uint16(0xC010)

	return &ControllerAR{
		ARUUID:        arUUID,
		DeviceMAC:     deviceMAC,
		DeviceIP:      deviceIP,
		LocalMAC:      localMAC,
		StationName:   stationName,
		State:         ControllerARStateClosed,
		OutputFrameID: outputFrameID,
		InputDataLen:  inputLen,
		OutputDataLen: outputLen,
		CycleTimeMs:   cycleTimeMs,
		Slots:         slots,
		rpc:           rpc,
		log:           log,
	}, nil
}

// Establish runs the full AR establishment: Connect → PrmEnd.
func (ar *ControllerAR) Establish(ctx context.Context) error {
	ar.log.Info("controller-ar: establishing AR",
		"device", ar.DeviceIP,
		"stationName", ar.StationName,
		"inputLen", ar.InputDataLen,
		"outputLen", ar.OutputDataLen,
	)

	// Step 1: Connect
	params := ConnectParams{
		ARUUID:        ar.ARUUID,
		LocalMAC:      ar.LocalMAC,
		StationName:   ar.StationName,
		InputDataLen:  ar.InputDataLen,
		OutputDataLen: ar.OutputDataLen,
		OutputFrameID: ar.OutputFrameID,
		CycleTimeMs:   ar.CycleTimeMs,
		Slots:         ar.Slots,
	}

	result, err := ar.rpc.Connect(ctx, params)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	ar.InputFrameID = result.InputFrameID
	ar.SessionKey = result.SessionKey
	ar.State = ControllerARStateConnected

	ar.log.Info("controller-ar: connected",
		"inputFrameID", fmt.Sprintf("0x%04X", ar.InputFrameID),
		"outputFrameID", fmt.Sprintf("0x%04X", ar.OutputFrameID),
	)

	// Step 2: PrmEnd
	err = ar.rpc.Control(ctx, ar.ARUUID, ar.SessionKey, profinet.ControlCmdPrmEnd)
	if err != nil {
		return fmt.Errorf("prmEnd: %w", err)
	}

	ar.State = ControllerARStateData
	ar.log.Info("controller-ar: AR in DATA state")

	return nil
}

// Release tears down the AR.
func (ar *ControllerAR) Release(ctx context.Context) error {
	if ar.rpc == nil {
		return nil
	}

	if ar.State != ControllerARStateClosed {
		err := ar.rpc.Release(ctx, ar.ARUUID, ar.SessionKey)
		ar.State = ControllerARStateClosed
		ar.rpc.Close()
		return err
	}

	ar.rpc.Close()
	return nil
}

// Close cleans up resources without sending Release.
func (ar *ControllerAR) Close() {
	if ar.rpc != nil {
		ar.rpc.Close()
	}
	ar.State = ControllerARStateClosed
}
