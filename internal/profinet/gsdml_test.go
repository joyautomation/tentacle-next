//go:build profinet || profinetall || profinetcontroller

package profinet

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

func testConfig() *ProfinetConfig {
	return &ProfinetConfig{
		StationName:   "tentacle-pn-1",
		InterfaceName: "eth1",
		VendorID:      0x1234,
		DeviceID:      0x0001,
		DeviceName:    "Tentacle PROFINET Bridge",
		CycleTimeUs:   1000,
		Slots: []SlotConfig{
			{
				SlotNumber:    1,
				ModuleIdentNo: 0x00000100,
				Subslots: []SubslotConfig{
					{
						SubslotNumber:    1,
						SubmoduleIdentNo: 0x00000101,
						Direction:        DirectionInput,
						InputSize:        10,
						Tags: []TagMapping{
							{TagID: "temp", ByteOffset: 0, Datatype: TypeFloat32, Source: "plc.data.gw1.temp"},
							{TagID: "count", ByteOffset: 4, Datatype: TypeUint32, Source: "plc.data.gw1.count"},
							{TagID: "status", ByteOffset: 8, Datatype: TypeUint16, Source: "plc.data.gw1.status"},
						},
					},
				},
			},
			{
				SlotNumber:    2,
				ModuleIdentNo: 0x00000200,
				Subslots: []SubslotConfig{
					{
						SubslotNumber:    1,
						SubmoduleIdentNo: 0x00000201,
						Direction:        DirectionOutput,
						OutputSize:       4,
						Tags: []TagMapping{
							{TagID: "setpoint", ByteOffset: 0, Datatype: TypeFloat32},
						},
					},
				},
			},
		},
	}
}

func TestGenerateGSDML_ValidXML(t *testing.T) {
	cfg := testConfig()
	data, err := GenerateGSDML(cfg)
	if err != nil {
		t.Fatalf("GenerateGSDML() error: %v", err)
	}

	// Should start with XML declaration
	if !strings.HasPrefix(string(data), "<?xml") {
		t.Error("missing XML declaration")
	}

	// Should be valid XML
	var doc gsdmlISO15745Profile
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("generated XML is not valid: %v", err)
	}
}

func TestGenerateGSDML_DeviceIdentity(t *testing.T) {
	cfg := testConfig()
	data, err := GenerateGSDML(cfg)
	if err != nil {
		t.Fatalf("GenerateGSDML() error: %v", err)
	}

	s := string(data)
	if !strings.Contains(s, `VendorID="0x1234"`) {
		t.Error("missing VendorID")
	}
	if !strings.Contains(s, `DeviceID="0x0001"`) {
		t.Error("missing DeviceID")
	}
	if !strings.Contains(s, `Value="JoyAutomation"`) {
		t.Error("missing VendorName")
	}
}

func TestGenerateGSDML_DAP(t *testing.T) {
	cfg := testConfig()
	data, err := GenerateGSDML(cfg)
	if err != nil {
		t.Fatalf("GenerateGSDML() error: %v", err)
	}

	s := string(data)
	if !strings.Contains(s, `ID="IDD_DAP"`) {
		t.Error("missing DAP item")
	}
	if !strings.Contains(s, fmt.Sprintf(`DNS_CompatibleName="%s"`, cfg.StationName)) {
		t.Error("missing station name in DAP")
	}
	if !strings.Contains(s, `SupportedRT_Classes="RT_CLASS_1"`) {
		t.Error("missing RT class")
	}
}

func TestGenerateGSDML_UserModules(t *testing.T) {
	cfg := testConfig()
	data, err := GenerateGSDML(cfg)
	if err != nil {
		t.Fatalf("GenerateGSDML() error: %v", err)
	}

	s := string(data)

	// Module 1 (input)
	if !strings.Contains(s, `ID="IDM_Mod1"`) {
		t.Error("missing module 1")
	}
	if !strings.Contains(s, `ModuleIdentNumber="0x00000100"`) {
		t.Error("missing module 1 ident number")
	}
	// Submodule input data
	if !strings.Contains(s, `Length="10"`) {
		t.Error("missing input data length 10")
	}

	// Module 2 (output)
	if !strings.Contains(s, `ID="IDM_Mod2"`) {
		t.Error("missing module 2")
	}
	if !strings.Contains(s, `ModuleIdentNumber="0x00000200"`) {
		t.Error("missing module 2 ident number")
	}
	// Submodule output data
	if !strings.Contains(s, `Length="4"`) {
		t.Error("missing output data length 4")
	}
}

func TestGenerateGSDML_InvalidConfig(t *testing.T) {
	cfg := &ProfinetConfig{} // missing required fields
	_, err := GenerateGSDML(cfg)
	if err == nil {
		t.Error("expected error for invalid config")
	}
}

func TestGenerateGSDML_NoUserModules(t *testing.T) {
	cfg := &ProfinetConfig{
		StationName:   "test",
		InterfaceName: "eth0",
		VendorID:      0x0001,
		DeviceID:      0x0001,
		DeviceName:    "Test",
	}
	data, err := GenerateGSDML(cfg)
	if err != nil {
		t.Fatalf("GenerateGSDML() error: %v", err)
	}

	// Should still be valid XML with just the DAP
	var doc gsdmlISO15745Profile
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("generated XML is not valid: %v", err)
	}

	// Should have DAP but PhysicalSlots should just be "0"
	dap := doc.ProfileBody.ApplicationProcess.DeviceAccessPointList.DeviceAccessPointItem[0]
	if dap.PhysicalSlots != "0" {
		t.Errorf("PhysicalSlots = %q, want %q", dap.PhysicalSlots, "0")
	}
}

func TestGSDMLFilename(t *testing.T) {
	cfg := &ProfinetConfig{DeviceName: "My Bridge 2000"}
	name := GSDMLFilename(cfg)
	if !strings.HasPrefix(name, "GSDML-V2.4-JoyAutomation-MyBridge2000-") {
		t.Errorf("unexpected filename: %s", name)
	}
	if !strings.HasSuffix(name, ".xml") {
		t.Error("filename should end with .xml")
	}
}

func TestGenerateGSDML_InputOutputDirection(t *testing.T) {
	cfg := &ProfinetConfig{
		StationName:   "test-io",
		InterfaceName: "eth0",
		VendorID:      0x0001,
		DeviceID:      0x0002,
		DeviceName:    "IO Test",
		Slots: []SlotConfig{
			{
				SlotNumber:    1,
				ModuleIdentNo: 0x00000100,
				Subslots: []SubslotConfig{
					{
						SubslotNumber:    1,
						SubmoduleIdentNo: 0x00000101,
						Direction:        DirectionInputOutput,
						InputSize:        4,
						OutputSize:       4,
						Tags: []TagMapping{
							{TagID: "sensor", ByteOffset: 0, Datatype: TypeFloat32, Source: "plc.data.gw1.sensor"},
							{TagID: "setpoint", ByteOffset: 0, Datatype: TypeFloat32},
						},
					},
				},
			},
		},
	}

	data, err := GenerateGSDML(cfg)
	if err != nil {
		t.Fatalf("GenerateGSDML() error: %v", err)
	}

	s := string(data)
	// Should have both Input and Output sections
	if !strings.Contains(s, "<Input>") {
		t.Error("missing Input section for inputOutput subslot")
	}
	if !strings.Contains(s, "<Output>") {
		t.Error("missing Output section for inputOutput subslot")
	}
}
