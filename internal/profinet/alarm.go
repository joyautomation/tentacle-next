//go:build profinet || profinetall

package profinet

import (
	"encoding/binary"
	"log/slog"
	"net"
	"sync"
)

// Alarm types.
const (
	AlarmTypeDiagnosis   uint16 = 0x0001
	AlarmTypeProcess     uint16 = 0x0002
	AlarmTypePull        uint16 = 0x0006
	AlarmTypePlug        uint16 = 0x0007
	AlarmTypeStatus      uint16 = 0x0008
	AlarmTypeUpdate      uint16 = 0x0009
	AlarmTypeReturnOfSub uint16 = 0x000B
)

// RTA (Real-Time Acyclic) frame constants.
const (
	FrameIDAlarmHigh uint16 = 0xFC01
	FrameIDAlarmLow  uint16 = 0xFE01

	RTAPDUTypeData uint16 = 0x0001
	RTAPDUTypeACK  uint16 = 0x0004
	RTAPDUTypeNACK uint16 = 0x0005
	RTAPDUTypeERR  uint16 = 0x0002
)

// AlarmHandler manages alarm notifications for an AR.
type AlarmHandler struct {
	transport *Transport
	ar        *AR
	log       *slog.Logger

	mu         sync.Mutex
	sendSeqNum uint16
	ackSeqNum  uint16
}

// NewAlarmHandler creates a new alarm handler.
func NewAlarmHandler(transport *Transport, ar *AR, log *slog.Logger) *AlarmHandler {
	return &AlarmHandler{
		transport: transport,
		ar:        ar,
		log:       log,
	}
}

// SendAlarm sends an alarm notification to the controller.
func (h *AlarmHandler) SendAlarm(alarmType uint16, api uint32, slot, subslot uint16, alarmSpecifier uint16) error {
	if h.ar.AlarmCR == nil {
		return nil
	}

	h.mu.Lock()
	seqNum := h.sendSeqNum
	h.sendSeqNum++
	ackSeq := h.ackSeqNum
	h.mu.Unlock()

	// Build alarm notification PDU
	payload := make([]byte, 20)
	binary.BigEndian.PutUint16(payload[0:2], alarmType)
	binary.BigEndian.PutUint32(payload[2:6], api)
	binary.BigEndian.PutUint16(payload[6:8], slot)
	binary.BigEndian.PutUint16(payload[8:10], subslot)
	binary.BigEndian.PutUint32(payload[10:14], 0) // ModuleIdentNumber
	binary.BigEndian.PutUint32(payload[14:18], 0) // SubmoduleIdentNumber
	binary.BigEndian.PutUint16(payload[18:20], alarmSpecifier)

	// Build RTA PDU
	frameID := FrameIDAlarmHigh
	if alarmType > 0x0001 {
		frameID = FrameIDAlarmLow
	}

	rtaPDU := make([]byte, 14+len(payload))
	binary.BigEndian.PutUint16(rtaPDU[0:2], frameID)
	binary.BigEndian.PutUint16(rtaPDU[2:4], RTAPDUTypeData)
	binary.BigEndian.PutUint16(rtaPDU[4:6], h.ar.AlarmCR.RemoteAlarmRef)
	binary.BigEndian.PutUint16(rtaPDU[6:8], h.ar.AlarmCR.LocalAlarmRef)
	binary.BigEndian.PutUint16(rtaPDU[8:10], seqNum)
	binary.BigEndian.PutUint16(rtaPDU[10:12], ackSeq)
	binary.BigEndian.PutUint16(rtaPDU[12:14], uint16(len(payload)))
	copy(rtaPDU[14:], payload)

	dstMAC := h.ar.InitiatorMAC
	if err := h.transport.SendFrame(dstMAC, rtaPDU); err != nil {
		h.log.Warn("alarm: send failed", "error", err)
		return err
	}

	h.log.Debug("alarm: sent notification", "type", alarmType, "slot", slot, "subslot", subslot)
	return nil
}

// HandleFrame processes incoming alarm-related RT frames (ACKs from controller).
func (h *AlarmHandler) HandleFrame(payload []byte, srcMAC net.HardwareAddr) {
	if len(payload) < 12 {
		return
	}

	pduType := binary.BigEndian.Uint16(payload[2:4])

	switch pduType {
	case RTAPDUTypeACK:
		ackSeqNum := binary.BigEndian.Uint16(payload[10:12])
		h.mu.Lock()
		h.ackSeqNum = ackSeqNum
		h.mu.Unlock()
		h.log.Debug("alarm: received ACK", "seqNum", ackSeqNum, "from", srcMAC)

	case RTAPDUTypeData:
		// Alarm notification from controller (e.g., alarm ACK with data)
		h.log.Debug("alarm: received notification from controller", "from", srcMAC)
	}
}

// IsAlarmFrame checks if the FrameID indicates an alarm frame.
func IsAlarmFrame(frameID uint16) bool {
	return frameID == FrameIDAlarmHigh || frameID == FrameIDAlarmLow
}

// MarshalAlarmAck creates an alarm ACK frame.
func MarshalAlarmAck(dstRef, srcRef, ackSeqNum uint16) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint16(buf[0:2], FrameIDAlarmHigh)
	binary.BigEndian.PutUint16(buf[2:4], RTAPDUTypeACK)
	binary.BigEndian.PutUint16(buf[4:6], dstRef)
	binary.BigEndian.PutUint16(buf[6:8], srcRef)
	binary.BigEndian.PutUint16(buf[8:10], 0) // SendSeqNum (0 for ACK)
	binary.BigEndian.PutUint16(buf[10:12], ackSeqNum)
	return buf
}
