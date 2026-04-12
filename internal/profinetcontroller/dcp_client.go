//go:build profinetcontroller || all

package profinetcontroller

import (
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	"github.com/joyautomation/tentacle/internal/profinet"
)

// DCPClient sends DCP discovery requests and collects responses.
// Responses are delivered by the scanner's central frame loop via HandleResponse.
type DCPClient struct {
	transport  *profinet.Transport
	log        *slog.Logger
	respCh     chan *DCPResponse
	xidCounter atomic.Uint32
}

// DCPResponse wraps a parsed DCP frame with the source MAC.
type DCPResponse struct {
	Frame  *profinet.DCPFrame
	SrcMAC net.HardwareAddr
}

// NewDCPClient creates a new DCP client.
func NewDCPClient(transport *profinet.Transport, log *slog.Logger) *DCPClient {
	return &DCPClient{
		transport: transport,
		log:       log,
		respCh:    make(chan *DCPResponse, 16),
	}
}

// HandleResponse is called by the frame loop when a DCP response arrives.
func (c *DCPClient) HandleResponse(payload []byte, srcMAC net.HardwareAddr) {
	frame, err := profinet.ParseDCPFrame(payload)
	if err != nil {
		c.log.Debug("dcp-client: parse error", "error", err)
		return
	}

	// Only handle responses
	if frame.ServiceType != profinet.DCPServiceTypeResponse {
		return
	}

	select {
	case c.respCh <- &DCPResponse{Frame: frame, SrcMAC: srcMAC}:
	default:
		// Drop if channel full
	}
}

// IdentifyAll sends a DCP Identify All and returns all responding devices.
func (c *DCPClient) IdentifyAll(ctx context.Context, timeout time.Duration) ([]DiscoveredDevice, error) {
	xid := c.xidCounter.Add(1)

	req := &profinet.DCPFrame{
		FrameID:       profinet.FrameIDDCPIdentReq,
		ServiceID:     profinet.DCPServiceIdentify,
		ServiceType:   profinet.DCPServiceTypeRequest,
		Xid:           xid,
		ResponseDelay: 1,
		Blocks: []profinet.DCPBlock{
			{Option: profinet.DCPOptionAllSelector, SubOption: 0x00FF},
		},
	}

	return c.sendAndCollect(ctx, req, timeout)
}

// IdentifyByName sends a DCP Identify filtered by station name.
func (c *DCPClient) IdentifyByName(ctx context.Context, stationName string, timeout time.Duration) (*DiscoveredDevice, error) {
	xid := c.xidCounter.Add(1)

	req := &profinet.DCPFrame{
		FrameID:       profinet.FrameIDDCPIdentReq,
		ServiceID:     profinet.DCPServiceIdentify,
		ServiceType:   profinet.DCPServiceTypeRequest,
		Xid:           xid,
		ResponseDelay: 1,
		Blocks: []profinet.DCPBlock{
			{
				Option:    profinet.DCPOptionDeviceProperties,
				SubOption: profinet.DCPSubOptionDevNameOfStation,
				Data:      []byte(stationName),
			},
		},
	}

	devices, err := c.sendAndCollect(ctx, req, timeout)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, nil
	}
	return &devices[0], nil
}

func (c *DCPClient) sendAndCollect(ctx context.Context, req *profinet.DCPFrame, timeout time.Duration) ([]DiscoveredDevice, error) {
	// Drain any stale responses
	for {
		select {
		case <-c.respCh:
		default:
			goto drained
		}
	}
drained:

	payload := profinet.MarshalDCPFrame(req)
	if err := c.transport.SendFrame(profinet.DCPMulticastAddr, payload); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var devices []DiscoveredDevice
	for {
		select {
		case <-ctx.Done():
			return devices, nil
		case resp := <-c.respCh:
			if resp.Frame.Xid != req.Xid {
				continue
			}
			if dev := parseDCPResponse(resp); dev != nil {
				devices = append(devices, *dev)
			}
		}
	}
}

func parseDCPResponse(resp *DCPResponse) *DiscoveredDevice {
	dev := &DiscoveredDevice{
		MAC: resp.SrcMAC,
	}

	for _, block := range resp.Frame.Blocks {
		switch {
		case block.Option == profinet.DCPOptionDeviceProperties && block.SubOption == profinet.DCPSubOptionDevNameOfStation:
			if len(block.Data) >= 2 {
				dev.StationName = string(block.Data[2:]) // skip 2-byte BlockInfo
			}
		case block.Option == profinet.DCPOptionDeviceProperties && block.SubOption == profinet.DCPSubOptionDevID:
			if len(block.Data) >= 6 {
				// BlockInfo(2) + VendorID(2) + DeviceID(2)
				dev.VendorID = binary.BigEndian.Uint16(block.Data[2:4])
				dev.DeviceID = binary.BigEndian.Uint16(block.Data[4:6])
			}
		case block.Option == profinet.DCPOptionIP && block.SubOption == profinet.DCPSubOptionIPSuite:
			ip, mask, gw, _ := profinet.ParseIPSuiteBlock(block.Data)
			dev.IP = ip
			dev.Mask = mask
			dev.Gateway = gw
		}
	}

	return dev
}
