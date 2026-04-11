//go:build profinet || profinetall

package profinet

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// GenerateGSDML produces a GSDML XML descriptor from a ProfinetConfig.
// The generated file can be imported into TIA Portal to define the IO Device.
func GenerateGSDML(cfg *ProfinetConfig) ([]byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	doc := buildGSDMLDocument(cfg)

	output, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("xml marshal: %w", err)
	}

	return append([]byte(xml.Header), output...), nil
}

// GSDMLFilename returns the conventional GSDML filename for a config.
func GSDMLFilename(cfg *ProfinetConfig) string {
	name := sanitizeGSDMLName(cfg.DeviceName)
	ts := time.Now().Format("20060102")
	return fmt.Sprintf("GSDML-V2.4-JoyAutomation-%s-%s.xml", name, ts)
}

func sanitizeGSDMLName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// GSDML XML types — structured to produce valid GSDML V2.4 output.

type gsdmlISO15745Profile struct {
	XMLName xml.Name `xml:"ISO15745Profile"`
	Xmlns   string   `xml:"xmlns,attr"`
	XSI     string   `xml:"xmlns:xsi,attr"`

	ProfileHeader gsdmlProfileHeader `xml:"ProfileHeader"`
	ProfileBody   gsdmlProfileBody   `xml:"ProfileBody"`
}

type gsdmlProfileHeader struct {
	ProfileIdentification string `xml:"ProfileIdentification"`
	ProfileRevision       string `xml:"ProfileRevision"`
	ProfileName           string `xml:"ProfileName"`
	ProfileSource         string `xml:"ProfileSource"`
	ProfileClassID        string `xml:"ProfileClassID"`
	ISO15745Reference     struct {
		ISO15745Part    int    `xml:"ISO15745Part"`
		ISO15745Edition int    `xml:"ISO15745Edition"`
		ProfileTechnology string `xml:"ProfileTechnology"`
	} `xml:"ISO15745Reference"`
}

type gsdmlProfileBody struct {
	XMLName       xml.Name           `xml:"ProfileBody"`
	DeviceIdentity gsdmlDeviceIdentity `xml:"DeviceIdentity"`
	DeviceFunction gsdmlDeviceFunction `xml:"DeviceFunction"`
	ApplicationProcess gsdmlApplicationProcess `xml:"ApplicationProcess"`
}

type gsdmlDeviceIdentity struct {
	VendorID   string `xml:"VendorID,attr"`
	DeviceID   string `xml:"DeviceID,attr"`
	InfoText   *gsdmlTextRef `xml:"InfoText,omitempty"`
	VendorName *gsdmlValue   `xml:"VendorName,omitempty"`
}

type gsdmlTextRef struct {
	TextId string `xml:"TextId,attr"`
}

type gsdmlValue struct {
	Value string `xml:"Value,attr"`
}

type gsdmlDeviceFunction struct {
	MainFamily   string `xml:"MainFamily,attr"`
	MainSubFamily string `xml:"MainSubFamily,attr,omitempty"`
}

type gsdmlApplicationProcess struct {
	DeviceAccessPointList gsdmlDAPList    `xml:"DeviceAccessPointList"`
	ModuleList            gsdmlModuleList `xml:"ModuleList"`
	SubmoduleList         gsdmlSubmoduleList `xml:"SubmoduleList"`
	ValueList             gsdmlValueList  `xml:"ValueList"`
}

type gsdmlDAPList struct {
	DeviceAccessPointItem []gsdmlDAPItem `xml:"DeviceAccessPointItem"`
}

type gsdmlDAPItem struct {
	ID                 string `xml:"ID,attr"`
	ModuleIdentNumber  string `xml:"ModuleIdentNumber,attr"`
	MinDeviceInterval  string `xml:"MinDeviceInterval,attr"`
	PhysicalSlots      string `xml:"PhysicalSlots,attr"`
	DNS_CompatibleName string `xml:"DNS_CompatibleName,attr"`
	FixedInSlots       string `xml:"FixedInSlots,attr"`

	ModuleInfo    *gsdmlModuleInfo      `xml:"ModuleInfo,omitempty"`
	UseableModules *gsdmlUseableModules `xml:"UseableModules,omitempty"`
	SystemDefinedSubmoduleList *gsdmlSystemSubmoduleList `xml:"SystemDefinedSubmoduleList,omitempty"`
}

type gsdmlModuleInfo struct {
	Name     *gsdmlTextRef `xml:"Name,omitempty"`
	InfoText *gsdmlTextRef `xml:"InfoText,omitempty"`
	OrderNumber *gsdmlValue `xml:"OrderNumber,omitempty"`
}

type gsdmlUseableModules struct {
	ModuleItemRef []gsdmlModuleItemRef `xml:"ModuleItemRef"`
}

type gsdmlModuleItemRef struct {
	ModuleItemTarget string `xml:"ModuleItemTarget,attr"`
	AllowedInSlots   string `xml:"AllowedInSlots,attr"`
}

type gsdmlSystemSubmoduleList struct {
	InterfaceSubmoduleItem []gsdmlInterfaceSubmodule `xml:"InterfaceSubmoduleItem"`
	PortSubmoduleItem      []gsdmlPortSubmodule      `xml:"PortSubmoduleItem"`
}

type gsdmlInterfaceSubmodule struct {
	ID                      string `xml:"ID,attr"`
	SubmoduleIdentNumber    string `xml:"SubmoduleIdentNumber,attr"`
	SubslotNumber           string `xml:"SubslotNumber,attr"`
	TextId                  string `xml:"TextId,attr"`
	SupportedRT_Classes     string `xml:"SupportedRT_Classes,attr"`
	NetworkInterface        string `xml:"NetworkInterface,attr"`
}

type gsdmlPortSubmodule struct {
	ID                   string `xml:"ID,attr"`
	SubmoduleIdentNumber string `xml:"SubmoduleIdentNumber,attr"`
	SubslotNumber        string `xml:"SubslotNumber,attr"`
	TextId               string `xml:"TextId,attr"`
	MaxPortRxDelay       string `xml:"MaxPortRxDelay,attr"`
	MaxPortTxDelay       string `xml:"MaxPortTxDelay,attr"`
}

type gsdmlModuleList struct {
	ModuleItem []gsdmlModuleItem `xml:"ModuleItem"`
}

type gsdmlModuleItem struct {
	ID                string `xml:"ID,attr"`
	ModuleIdentNumber string `xml:"ModuleIdentNumber,attr"`
	ModuleInfo        *gsdmlModuleInfo `xml:"ModuleInfo,omitempty"`
	VirtualSubmoduleList *gsdmlVirtualSubmoduleList `xml:"VirtualSubmoduleList,omitempty"`
}

type gsdmlVirtualSubmoduleList struct {
	VirtualSubmoduleItem []gsdmlVirtualSubmoduleItem `xml:"VirtualSubmoduleItem"`
}

type gsdmlVirtualSubmoduleItem struct {
	ID                   string `xml:"ID,attr"`
	SubmoduleIdentNumber string `xml:"SubmoduleIdentNumber,attr"`
	FixedInSubslots      string `xml:"FixedInSubslots,attr,omitempty"`
	MayIssueProcessAlarm string `xml:"MayIssueProcessAlarm,attr,omitempty"`
	ModuleInfo           *gsdmlModuleInfo `xml:"ModuleInfo,omitempty"`
	IOData               *gsdmlIOData     `xml:"IOData,omitempty"`
}

type gsdmlIOData struct {
	Input  *gsdmlIODataDir `xml:"Input,omitempty"`
	Output *gsdmlIODataDir `xml:"Output,omitempty"`
}

type gsdmlIODataDir struct {
	DataItem []gsdmlDataItem `xml:"DataItem"`
}

type gsdmlDataItem struct {
	DataType   string `xml:"DataType,attr"`
	Length     string `xml:"Length,attr"`
	TextId     string `xml:"TextId,attr"`
}

type gsdmlSubmoduleList struct {
	// Empty — all submodules are defined inline within VirtualSubmoduleList
}

type gsdmlValueList struct {
	Value []gsdmlValueEntry `xml:"Value"`
}

type gsdmlValueEntry struct {
	ID   string `xml:"ID,attr"`
	Text string `xml:",chardata"`
}

func buildGSDMLDocument(cfg *ProfinetConfig) gsdmlISO15745Profile {
	values := []gsdmlValueEntry{
		{ID: "IDT_INFO_Device", Text: cfg.DeviceName},
		{ID: "IDT_NAME_DAP", Text: "DAP"},
		{ID: "IDT_INFO_DAP", Text: "Device Access Point"},
		{ID: "IDT_NAME_Interface", Text: "Interface"},
		{ID: "IDT_NAME_Port1", Text: "Port 1"},
	}

	// Compute slot range for PhysicalSlots and module refs
	maxSlot := 0
	for _, slot := range cfg.Slots {
		if int(slot.SlotNumber) > maxSlot {
			maxSlot = int(slot.SlotNumber)
		}
	}
	if maxSlot == 0 {
		maxSlot = 1
	}

	// Build slot range string "1..N"
	slotRange := fmt.Sprintf("1..%d", maxSlot)

	// Build module items and refs
	var moduleItems []gsdmlModuleItem
	var moduleRefs []gsdmlModuleItemRef

	for _, slot := range cfg.Slots {
		if slot.SlotNumber == 0 {
			continue // DAP handled separately
		}

		moduleID := fmt.Sprintf("IDM_Mod%d", slot.SlotNumber)
		moduleNameID := fmt.Sprintf("IDT_NAME_Mod%d", slot.SlotNumber)
		values = append(values, gsdmlValueEntry{
			ID:   moduleNameID,
			Text: fmt.Sprintf("Module Slot %d", slot.SlotNumber),
		})

		var virtualSubs []gsdmlVirtualSubmoduleItem
		for _, sub := range slot.Subslots {
			subID := fmt.Sprintf("IDS_Mod%d_Sub%d", slot.SlotNumber, sub.SubslotNumber)
			subNameID := fmt.Sprintf("IDT_NAME_Mod%d_Sub%d", slot.SlotNumber, sub.SubslotNumber)
			values = append(values, gsdmlValueEntry{
				ID:   subNameID,
				Text: fmt.Sprintf("Slot %d Subslot %d", slot.SlotNumber, sub.SubslotNumber),
			})

			ioData := buildIOData(slot.SlotNumber, sub)

			virtualSubs = append(virtualSubs, gsdmlVirtualSubmoduleItem{
				ID:                   subID,
				SubmoduleIdentNumber: fmt.Sprintf("0x%08X", sub.SubmoduleIdentNo),
				FixedInSubslots:      fmt.Sprintf("%d", sub.SubslotNumber),
				MayIssueProcessAlarm: "true",
				ModuleInfo: &gsdmlModuleInfo{
					Name: &gsdmlTextRef{TextId: subNameID},
				},
				IOData: ioData,
			})
		}

		moduleItems = append(moduleItems, gsdmlModuleItem{
			ID:                moduleID,
			ModuleIdentNumber: fmt.Sprintf("0x%08X", slot.ModuleIdentNo),
			ModuleInfo: &gsdmlModuleInfo{
				Name: &gsdmlTextRef{TextId: moduleNameID},
			},
			VirtualSubmoduleList: &gsdmlVirtualSubmoduleList{
				VirtualSubmoduleItem: virtualSubs,
			},
		})

		moduleRefs = append(moduleRefs, gsdmlModuleItemRef{
			ModuleItemTarget: moduleID,
			AllowedInSlots:   fmt.Sprintf("%d", slot.SlotNumber),
		})
	}

	// Compute MinDeviceInterval from cycle time (in 31.25µs units)
	minInterval := cfg.CycleTimeUs * 32 / 1000 // CycleTimeUs / 31.25
	if minInterval < 32 {
		minInterval = 32 // minimum 1ms
	}

	doc := gsdmlISO15745Profile{
		Xmlns: "http://www.profibus.com/GSDML/2003/11/DeviceProfile",
		XSI:   "http://www.w3.org/2001/XMLSchema-instance",

		ProfileHeader: gsdmlProfileHeader{
			ProfileIdentification: "PROFINET Device Profile",
			ProfileRevision:       "1.00",
			ProfileName:           cfg.DeviceName,
			ProfileSource:         "JoyAutomation",
			ProfileClassID:        "Device",
			ISO15745Reference: struct {
				ISO15745Part      int    `xml:"ISO15745Part"`
				ISO15745Edition   int    `xml:"ISO15745Edition"`
				ProfileTechnology string `xml:"ProfileTechnology"`
			}{
				ISO15745Part:      4,
				ISO15745Edition:   1,
				ProfileTechnology: "GSDML",
			},
		},

		ProfileBody: gsdmlProfileBody{
			DeviceIdentity: gsdmlDeviceIdentity{
				VendorID: fmt.Sprintf("0x%04X", cfg.VendorID),
				DeviceID: fmt.Sprintf("0x%04X", cfg.DeviceID),
				InfoText: &gsdmlTextRef{TextId: "IDT_INFO_Device"},
				VendorName: &gsdmlValue{Value: "JoyAutomation"},
			},
			DeviceFunction: gsdmlDeviceFunction{
				MainFamily: "I/O",
			},
			ApplicationProcess: gsdmlApplicationProcess{
				DeviceAccessPointList: gsdmlDAPList{
					DeviceAccessPointItem: []gsdmlDAPItem{
						{
							ID:                 "IDD_DAP",
							ModuleIdentNumber:  "0x00000001",
							MinDeviceInterval:  fmt.Sprintf("%d", minInterval),
							PhysicalSlots:      fmt.Sprintf("0..%d", maxSlot),
							DNS_CompatibleName: cfg.StationName,
							FixedInSlots:       "0",
							ModuleInfo: &gsdmlModuleInfo{
								Name:     &gsdmlTextRef{TextId: "IDT_NAME_DAP"},
								InfoText: &gsdmlTextRef{TextId: "IDT_INFO_DAP"},
								OrderNumber: &gsdmlValue{Value: "TENTACLE-PN"},
							},
							UseableModules: &gsdmlUseableModules{
								ModuleItemRef: moduleRefs,
							},
							SystemDefinedSubmoduleList: &gsdmlSystemSubmoduleList{
								InterfaceSubmoduleItem: []gsdmlInterfaceSubmodule{
									{
										ID:                   "IDS_Interface",
										SubmoduleIdentNumber: "0x00000001",
										SubslotNumber:        "32768",
										TextId:               "IDT_NAME_Interface",
										SupportedRT_Classes:  "RT_CLASS_1",
										NetworkInterface:     "0",
									},
								},
								PortSubmoduleItem: []gsdmlPortSubmodule{
									{
										ID:                   "IDS_Port1",
										SubmoduleIdentNumber: "0x00000002",
										SubslotNumber:        "32769",
										TextId:               "IDT_NAME_Port1",
										MaxPortRxDelay:       "350",
										MaxPortTxDelay:       "350",
									},
								},
							},
						},
					},
				},
				ModuleList: gsdmlModuleList{
					ModuleItem: moduleItems,
				},
				ValueList: gsdmlValueList{
					Value: values,
				},
			},
		},
	}

	// If no user modules, ensure empty slices
	if len(slotRange) > 0 && len(moduleItems) == 0 {
		doc.ProfileBody.ApplicationProcess.DeviceAccessPointList.DeviceAccessPointItem[0].PhysicalSlots = "0"
		doc.ProfileBody.ApplicationProcess.DeviceAccessPointList.DeviceAccessPointItem[0].UseableModules = nil
	}

	return doc
}

func buildIOData(slotNum uint16, sub SubslotConfig) *gsdmlIOData {
	ioData := &gsdmlIOData{}

	if sub.InputSize > 0 && (sub.Direction == DirectionInput || sub.Direction == DirectionInputOutput) {
		ioData.Input = &gsdmlIODataDir{
			DataItem: []gsdmlDataItem{
				{
					DataType: "OctetString",
					Length:   fmt.Sprintf("%d", sub.InputSize),
					TextId:   fmt.Sprintf("IDT_IO_Slot%d_Sub%d_In", slotNum, sub.SubslotNumber),
				},
			},
		}
	}

	if sub.OutputSize > 0 && (sub.Direction == DirectionOutput || sub.Direction == DirectionInputOutput) {
		ioData.Output = &gsdmlIODataDir{
			DataItem: []gsdmlDataItem{
				{
					DataType: "OctetString",
					Length:   fmt.Sprintf("%d", sub.OutputSize),
					TextId:   fmt.Sprintf("IDT_IO_Slot%d_Sub%d_Out", slotNum, sub.SubslotNumber),
				},
			},
		}
	}

	if ioData.Input == nil && ioData.Output == nil {
		return nil
	}
	return ioData
}
