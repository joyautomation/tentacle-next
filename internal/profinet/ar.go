//go:build profinet || profinetall || profinetcontroller

package profinet

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sync"
)

// AR states.
const (
	ARStateClosed       = "CLOSED"
	ARStateConnected    = "CONNECTED"      // AR block accepted
	ARStateParameterize = "PARAMETERIZE"   // Write requests being received
	ARStateWaitPrmEnd   = "WAIT_PRM_END"   // Waiting for PrmEnd control
	ARStateWaitAppReady = "WAIT_APP_READY" // PrmEnd received, device preparing
	ARStateData         = "DATA"           // Cyclic exchange active
)

// AR represents an Application Relationship with a controller.
type AR struct {
	ARUUID       [16]byte
	ARType       uint16
	SessionKey   uint16
	InitiatorMAC net.HardwareAddr
	InitiatorAddr *net.UDPAddr
	ActivityUUID [16]byte
	State        string

	// Negotiated IOCRs
	InputIOCR  *IOCRInfo
	OutputIOCR *IOCRInfo

	// Alarm CR
	AlarmCR *AlarmCRInfo

	// Expected submodules from controller
	ExpectedSubmodules *ExpectedSubmoduleReq
}

// IOCRInfo holds negotiated IOCR parameters.
type IOCRInfo struct {
	IOCRType        uint16
	IOCRReference   uint16
	FrameID         uint16 // Assigned by device for input, by controller for output
	DataLength      uint16
	SendClockFactor uint16
	ReductionRatio  uint16
	WatchdogFactor  uint16
	DataHoldFactor  uint16
	APIs            []IOCRAPIEntry
}

// AlarmCRInfo holds alarm CR parameters.
type AlarmCRInfo struct {
	AlarmCRType     uint16
	LocalAlarmRef   uint16
	RemoteAlarmRef  uint16
	MaxAlarmDataLen uint16
}

// ARManager manages active Application Relationships.
type ARManager struct {
	cfg       *ProfinetConfig
	localMAC  net.HardwareAddr
	log       *slog.Logger

	// Callbacks for cyclic start/stop
	onCyclicStart func(ar *AR)
	onCyclicStop  func(ar *AR)

	mu  sync.Mutex
	ars map[[16]byte]*AR // ARUUID -> AR

	// Frame ID counter for device-assigned frame IDs (input CRs)
	nextFrameID uint16
}

// NewARManager creates a new AR manager.
func NewARManager(cfg *ProfinetConfig, localMAC net.HardwareAddr, log *slog.Logger) *ARManager {
	return &ARManager{
		cfg:         cfg,
		localMAC:    localMAC,
		log:         log,
		ars:         make(map[[16]byte]*AR),
		nextFrameID: 0xC001, // RT Class 1 range: 0xC000-0xF7FF
	}
}

func (m *ARManager) allocFrameID() uint16 {
	id := m.nextFrameID
	m.nextFrameID++
	if m.nextFrameID > 0xF7FF {
		m.nextFrameID = 0xC001
	}
	return id
}

// HandleConnect processes a Connect RPC request.
func (m *ARManager) HandleConnect(blocks []PNIOBlock, from *net.UDPAddr, activityUUID [16]byte) ([]byte, PNIOStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var arBlock *ARBlockReq
	var iocrBlocks []*IOCRBlockReq
	var alarmBlock *AlarmCRBlockReq
	var expectedBlock *ExpectedSubmoduleReq

	for _, b := range blocks {
		switch b.Type {
		case BlockTypeARBlockReq:
			parsed, err := ParseARBlockReq(b.Data)
			if err != nil {
				m.log.Warn("ar: failed to parse ARBlockReq", "error", err)
				return nil, PNIOStatus{0xDE, 0x81, 0x01, 0x00}
			}
			arBlock = parsed
		case BlockTypeIOCRBlockReq:
			parsed, err := ParseIOCRBlockReq(b.Data)
			if err != nil {
				m.log.Warn("ar: failed to parse IOCRBlockReq", "error", err)
				continue
			}
			iocrBlocks = append(iocrBlocks, parsed)
		case BlockTypeAlarmCRBlockReq:
			parsed, err := ParseAlarmCRBlockReq(b.Data)
			if err != nil {
				m.log.Warn("ar: failed to parse AlarmCRBlockReq", "error", err)
				continue
			}
			alarmBlock = parsed
		case BlockTypeExpectedSubmoduleReq:
			parsed, err := ParseExpectedSubmoduleReq(b.Data)
			if err != nil {
				m.log.Warn("ar: failed to parse ExpectedSubmoduleReq", "error", err)
				continue
			}
			expectedBlock = parsed
		}
	}

	if arBlock == nil {
		return nil, PNIOStatus{0xDE, 0x81, 0x01, 0x00}
	}

	// Create AR
	ar := &AR{
		ARUUID:             arBlock.ARUUID,
		ARType:             arBlock.ARType,
		SessionKey:         arBlock.SessionKey,
		InitiatorMAC:       arBlock.CMInitiatorMAC,
		InitiatorAddr:      from,
		ActivityUUID:       activityUUID,
		State:              ARStateConnected,
		ExpectedSubmodules: expectedBlock,
	}

	m.log.Info("ar: new AR",
		"uuid", fmt.Sprintf("%x", arBlock.ARUUID),
		"type", arBlock.ARType,
		"station", arBlock.CMInitiatorStationName,
		"from", from,
	)

	// Build response blocks
	var respBuf []byte

	// AR Block Response
	respBuf = append(respBuf, MarshalARBlockRes(
		arBlock.ARType, arBlock.ARUUID, arBlock.SessionKey,
		m.localMAC, PNIOCMPort,
	)...)

	// IOCR Block Responses
	for _, iocr := range iocrBlocks {
		switch iocr.IOCRType {
		case IOCRTypeInput:
			frameID := m.allocFrameID()
			ar.InputIOCR = &IOCRInfo{
				IOCRType:        iocr.IOCRType,
				IOCRReference:   iocr.IOCRReference,
				FrameID:         frameID,
				DataLength:      iocr.DataLength,
				SendClockFactor: iocr.SendClockFactor,
				ReductionRatio:  iocr.ReductionRatio,
				WatchdogFactor:  iocr.WatchdogFactor,
				DataHoldFactor:  iocr.DataHoldFactor,
				APIs:            iocr.APIs,
			}
			respBuf = append(respBuf, MarshalIOCRBlockRes(iocr.IOCRType, iocr.IOCRReference, frameID)...)

		case IOCRTypeOutput:
			ar.OutputIOCR = &IOCRInfo{
				IOCRType:        iocr.IOCRType,
				IOCRReference:   iocr.IOCRReference,
				FrameID:         iocr.FrameID,
				DataLength:      iocr.DataLength,
				SendClockFactor: iocr.SendClockFactor,
				ReductionRatio:  iocr.ReductionRatio,
				WatchdogFactor:  iocr.WatchdogFactor,
				DataHoldFactor:  iocr.DataHoldFactor,
				APIs:            iocr.APIs,
			}
			respBuf = append(respBuf, MarshalIOCRBlockRes(iocr.IOCRType, iocr.IOCRReference, iocr.FrameID)...)
		}
	}

	// Alarm CR Response
	if alarmBlock != nil {
		ar.AlarmCR = &AlarmCRInfo{
			AlarmCRType:     alarmBlock.AlarmCRType,
			RemoteAlarmRef:  alarmBlock.LocalAlarmRef,
			LocalAlarmRef:   0x0001,
			MaxAlarmDataLen: alarmBlock.MaxAlarmDataLen,
		}
		respBuf = append(respBuf, MarshalAlarmCRBlockRes(
			alarmBlock.AlarmCRType, 0x0001, alarmBlock.MaxAlarmDataLen,
		)...)
	}

	// Module Diff Block (no differences — we support whatever the controller expects)
	respBuf = append(respBuf, MarshalModuleDiffBlock(0)...)

	// Pad response to 4-byte alignment
	for len(respBuf)%4 != 0 {
		respBuf = append(respBuf, 0)
	}

	// Store AR
	m.ars[arBlock.ARUUID] = ar

	return respBuf, PNIOStatusOK
}

// HandleRelease processes a Release RPC request.
func (m *ARManager) HandleRelease(blocks []PNIOBlock, objectUUID [16]byte) ([]byte, PNIOStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ar, ok := m.ars[objectUUID]
	arUUID := objectUUID
	if !ok {
		// Try finding by AR UUID in control blocks
		for _, b := range blocks {
			if b.Type == BlockTypeIODControlReq && len(b.Data) >= 16 {
				var uuid [16]byte
				copy(uuid[:], b.Data[0:16])
				ar, ok = m.ars[uuid]
				if ok {
					arUUID = uuid
					break
				}
			}
		}
	}

	if ar == nil {
		return nil, PNIOStatus{0xDE, 0x81, 0x04, 0x00}
	}

	m.log.Info("ar: Release", "uuid", fmt.Sprintf("%x", arUUID), "state", ar.State)

	if m.onCyclicStop != nil {
		m.onCyclicStop(ar)
	}

	ar.State = ARStateClosed
	delete(m.ars, arUUID)

	respBuf := MarshalIODControlRes(arUUID, ar.SessionKey, ControlCmdDone)
	return respBuf, PNIOStatusOK
}

// HandleWrite processes a Write RPC request (parameterization).
func (m *ARManager) HandleWrite(blocks []PNIOBlock, objectUUID [16]byte) ([]byte, PNIOStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ar := m.findAR(objectUUID)
	if ar != nil && ar.State == ARStateConnected {
		ar.State = ARStateParameterize
	}

	// Build write response blocks — acknowledge each write request header
	var respBuf []byte
	for _, b := range blocks {
		if b.Type == BlockTypeIODWriteReqHeader {
			resp := MarshalPNIOBlock(BlockTypeIODWriteResHeader, 1, 0, b.Data)
			respBuf = append(respBuf, resp...)
		}
	}

	return respBuf, PNIOStatusOK
}

// HandleRead processes a Read RPC request.
func (m *ARManager) HandleRead(blocks []PNIOBlock, objectUUID [16]byte) ([]byte, PNIOStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var respBuf []byte
	for _, b := range blocks {
		if b.Type == BlockTypeIODReadReqHeader && len(b.Data) >= 24 {
			index := binary.BigEndian.Uint16(b.Data[8:10])

			switch {
			case index == 0xAFF0: // I&M 0
				im0 := m.buildIM0()
				respBuf = append(respBuf, MarshalPNIOBlock(BlockTypeIODReadResHeader, 1, 0, b.Data)...)
				respBuf = append(respBuf, im0...)
			default:
				respBuf = append(respBuf, MarshalPNIOBlock(BlockTypeIODReadResHeader, 1, 0, b.Data)...)
			}
		}
	}

	return respBuf, PNIOStatusOK
}

// HandleControl processes a Control RPC request (PrmEnd, ApplicationReady, etc).
func (m *ARManager) HandleControl(blocks []PNIOBlock, objectUUID [16]byte) ([]byte, PNIOStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var respBuf []byte

	for _, b := range blocks {
		if b.Type != BlockTypeIODControlReq && b.Type != BlockTypeARRPCBlockReq {
			continue
		}

		if len(b.Data) < 22 {
			continue
		}

		// Parse control block: ARUUID(16) + SessionKey(2) + padding(2) + ControlCommand(2)
		var blockARUUID [16]byte
		copy(blockARUUID[:], b.Data[0:16])
		sessionKey := binary.BigEndian.Uint16(b.Data[16:18])
		controlCmd := binary.BigEndian.Uint16(b.Data[20:22])

		ar := m.findARByUUID(blockARUUID)
		if ar == nil {
			m.log.Warn("ar: control for unknown AR", "uuid", fmt.Sprintf("%x", blockARUUID))
			return nil, PNIOStatus{0xDE, 0x81, 0x04, 0x00}
		}

		switch controlCmd {
		case ControlCmdPrmEnd:
			m.log.Info("ar: PrmEnd received", "uuid", fmt.Sprintf("%x", blockARUUID))
			ar.State = ARStateWaitAppReady

			respBuf = append(respBuf, MarshalIODControlRes(blockARUUID, sessionKey, ControlCmdDone)...)

			// Transition to DATA state and start cyclic exchange
			go m.applicationReady(blockARUUID)

		case ControlCmdApplicationReady:
			m.log.Info("ar: ApplicationReady from controller", "uuid", fmt.Sprintf("%x", blockARUUID))
			respBuf = append(respBuf, MarshalIODControlRes(blockARUUID, sessionKey, ControlCmdDone)...)

		case ControlCmdRelease:
			m.log.Info("ar: Release via control", "uuid", fmt.Sprintf("%x", blockARUUID))
			if m.onCyclicStop != nil {
				m.onCyclicStop(ar)
			}
			ar.State = ARStateClosed
			delete(m.ars, blockARUUID)
			respBuf = append(respBuf, MarshalIODControlRes(blockARUUID, sessionKey, ControlCmdDone)...)
		}
	}

	return respBuf, PNIOStatusOK
}

// applicationReady is called after PrmEnd to transition the AR to DATA state.
func (m *ARManager) applicationReady(arUUID [16]byte) {
	m.mu.Lock()
	ar, ok := m.ars[arUUID]
	if !ok || ar.State != ARStateWaitAppReady {
		m.mu.Unlock()
		return
	}

	ar.State = ARStateData
	m.log.Info("ar: entering DATA state", "uuid", fmt.Sprintf("%x", arUUID))

	if m.onCyclicStart != nil {
		m.mu.Unlock()
		m.onCyclicStart(ar)
		return
	}
	m.mu.Unlock()
}

func (m *ARManager) findAR(objectUUID [16]byte) *AR {
	if ar, ok := m.ars[objectUUID]; ok {
		return ar
	}
	// If only one AR exists, return it (common case)
	if len(m.ars) == 1 {
		for _, ar := range m.ars {
			return ar
		}
	}
	return nil
}

func (m *ARManager) findARByUUID(arUUID [16]byte) *AR {
	return m.ars[arUUID]
}

// GetAR returns an AR by UUID (thread-safe).
func (m *ARManager) GetAR(arUUID [16]byte) *AR {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ars[arUUID]
}

// GetActiveAR returns the first active AR (for simple single-AR scenarios).
func (m *ARManager) GetActiveAR() *AR {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ar := range m.ars {
		if ar.State == ARStateData {
			return ar
		}
	}
	return nil
}

// SetCyclicCallbacks sets the callbacks for cyclic start/stop.
func (m *ARManager) SetCyclicCallbacks(onStart, onStop func(ar *AR)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onCyclicStart = onStart
	m.onCyclicStop = onStop
}

func (m *ARManager) buildIM0() []byte {
	im0 := &IM0Data{
		VendorID:         m.cfg.VendorID,
		HWRevision:       1,
		SWRevisionPrefix: 'V',
		SWRevision:       [3]byte{1, 0, 0},
		RevisionCounter:  0,
		ProfileID:        0x0000,
		ProfileSpecType:  0x0003,
		IMVersion:        0x0101,
		IMSupported:      0x001E,
	}
	copy(im0.OrderID[:], []byte(m.cfg.DeviceName))
	copy(im0.IMSerialNumber[:], []byte("0001"))
	return MarshalIM0Block(im0)
}

// ActiveARCount returns the number of active ARs.
func (m *ARManager) ActiveARCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.ars)
}
