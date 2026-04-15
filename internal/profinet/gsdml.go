//go:build profinet || profinetcontroller || all

package profinet

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// GenerateGSDML produces a GSDML XML descriptor from a ProfinetConfig.
// The generated file can be imported into TIA Portal or PRONETA to define the IO Device.
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
	return fmt.Sprintf("GSDML-V2.35-JoyAutomation-%s-%s.xml", name, ts)
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

// GSDML XML types — structured to produce valid GSDML V2.35 output.
// Modeled after known-good p-net GSDML files that PRONETA accepts.

type gsdmlISO15745Profile struct {
	XMLName         xml.Name           `xml:"ISO15745Profile"`
	Xmlns           string             `xml:"xmlns,attr"`
	XSI             string             `xml:"xmlns:xsi,attr"`
	SchemaLocation  string             `xml:"xsi:schemaLocation,attr"`
	ProfileHeader   gsdmlProfileHeader `xml:"ProfileHeader"`
	ProfileBody     gsdmlProfileBody   `xml:"ProfileBody"`
}

type gsdmlProfileHeader struct {
	ProfileIdentification string `xml:"ProfileIdentification"`
	ProfileRevision       string `xml:"ProfileRevision"`
	ProfileName           string `xml:"ProfileName"`
	ProfileSource         string `xml:"ProfileSource"`
	ProfileClassID        string `xml:"ProfileClassID"`
	ISO15745Reference     struct {
		ISO15745Part      int    `xml:"ISO15745Part"`
		ISO15745Edition   int    `xml:"ISO15745Edition"`
		ProfileTechnology string `xml:"ProfileTechnology"`
	} `xml:"ISO15745Reference"`
}

type gsdmlProfileBody struct {
	DeviceIdentity     gsdmlDeviceIdentity     `xml:"DeviceIdentity"`
	DeviceFunction     gsdmlDeviceFunction     `xml:"DeviceFunction"`
	ApplicationProcess gsdmlApplicationProcess `xml:"ApplicationProcess"`
}

type gsdmlDeviceIdentity struct {
	VendorID   string        `xml:"VendorID,attr"`
	DeviceID   string        `xml:"DeviceID,attr"`
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
	Family gsdmlFamily `xml:"Family"`
}

type gsdmlFamily struct {
	MainFamily    string `xml:"MainFamily,attr"`
	ProductFamily string `xml:"ProductFamily,attr,omitempty"`
}

type gsdmlApplicationProcess struct {
	DeviceAccessPointList gsdmlDAPList          `xml:"DeviceAccessPointList"`
	ModuleList            gsdmlModuleList       `xml:"ModuleList"`
	ExternalTextList      gsdmlExternalTextList `xml:"ExternalTextList"`
}

type gsdmlDAPList struct {
	DeviceAccessPointItem []gsdmlDAPItem `xml:"DeviceAccessPointItem"`
}

type gsdmlDAPItem struct {
	ID                     string `xml:"ID,attr"`
	PNIO_Version           string `xml:"PNIO_Version,attr"`
	PhysicalSlots          string `xml:"PhysicalSlots,attr"`
	ModuleIdentNumber      string `xml:"ModuleIdentNumber,attr"`
	MinDeviceInterval      string `xml:"MinDeviceInterval,attr"`
	DNS_CompatibleName     string `xml:"DNS_CompatibleName,attr"`
	FixedInSlots           string `xml:"FixedInSlots,attr"`
	ObjectUUID_LocalIndex  string `xml:"ObjectUUID_LocalIndex,attr"`
	DeviceAccessSupported  string `xml:"DeviceAccessSupported,attr"`
	NumberOfDeviceAccessAR string `xml:"NumberOfDeviceAccessAR,attr"`
	MultipleWriteSupported string `xml:"MultipleWriteSupported,attr"`

	ModuleInfo                 *gsdmlModuleInfo          `xml:"ModuleInfo,omitempty"`
	IOConfigData               *gsdmlIOConfigData        `xml:"IOConfigData,omitempty"`
	UseableModules             *gsdmlUseableModules      `xml:"UseableModules,omitempty"`
	VirtualSubmoduleList       *gsdmlVirtualSubmoduleList `xml:"VirtualSubmoduleList,omitempty"`
	SystemDefinedSubmoduleList *gsdmlSystemSubmoduleList `xml:"SystemDefinedSubmoduleList,omitempty"`
}

type gsdmlIOConfigData struct {
	MaxInputLength  string `xml:"MaxInputLength,attr"`
	MaxOutputLength string `xml:"MaxOutputLength,attr"`
}

type gsdmlModuleInfo struct {
	Name        *gsdmlTextRef `xml:"Name,omitempty"`
	InfoText    *gsdmlTextRef `xml:"InfoText,omitempty"`
	VendorName  *gsdmlValue   `xml:"VendorName,omitempty"`
	OrderNumber *gsdmlValue   `xml:"OrderNumber,omitempty"`
}

type gsdmlUseableModules struct {
	ModuleItemRef []gsdmlModuleItemRef `xml:"ModuleItemRef"`
}

type gsdmlModuleItemRef struct {
	ModuleItemTarget string `xml:"ModuleItemTarget,attr"`
	AllowedInSlots   string `xml:"AllowedInSlots,attr,omitempty"`
	FixedInSlots     string `xml:"FixedInSlots,attr,omitempty"`
}

type gsdmlSystemSubmoduleList struct {
	InterfaceSubmoduleItem []gsdmlInterfaceSubmodule `xml:"InterfaceSubmoduleItem"`
	PortSubmoduleItem      []gsdmlPortSubmodule      `xml:"PortSubmoduleItem"`
}

type gsdmlInterfaceSubmodule struct {
	ID                   string                    `xml:"ID,attr"`
	SubmoduleIdentNumber string                    `xml:"SubmoduleIdentNumber,attr"`
	SubslotNumber        string                    `xml:"SubslotNumber,attr"`
	TextId               string                    `xml:"TextId,attr"`
	SupportedRT_Classes  string                    `xml:"SupportedRT_Classes,attr"`
	SupportedProtocols   string                    `xml:"SupportedProtocols,attr,omitempty"`
	NetworkInterface     string                    `xml:"NetworkInterface,attr,omitempty"`
	ApplicationRelations *gsdmlApplicationRelations `xml:"ApplicationRelations,omitempty"`
}

type gsdmlApplicationRelations struct {
	StartupMode      string              `xml:"StartupMode,attr"`
	TimingProperties gsdmlTimingProperties `xml:"TimingProperties"`
}

type gsdmlTimingProperties struct {
	SendClock      string `xml:"SendClock,attr"`
	ReductionRatio string `xml:"ReductionRatio,attr"`
}

type gsdmlPortSubmodule struct {
	ID                   string           `xml:"ID,attr"`
	SubmoduleIdentNumber string           `xml:"SubmoduleIdentNumber,attr"`
	SubslotNumber        string           `xml:"SubslotNumber,attr"`
	TextId               string           `xml:"TextId,attr"`
	MaxPortRxDelay       string           `xml:"MaxPortRxDelay,attr"`
	MaxPortTxDelay       string           `xml:"MaxPortTxDelay,attr"`
	MAUTypeList          *gsdmlMAUTypeList `xml:"MAUTypeList,omitempty"`
}

type gsdmlMAUTypeList struct {
	MAUTypeItem []gsdmlMAUTypeItem `xml:"MAUTypeItem"`
}

type gsdmlMAUTypeItem struct {
	Value string `xml:"Value,attr"`
}

type gsdmlModuleList struct {
	ModuleItem []gsdmlModuleItem `xml:"ModuleItem"`
}

type gsdmlModuleItem struct {
	ID                   string                     `xml:"ID,attr"`
	ModuleIdentNumber    string                     `xml:"ModuleIdentNumber,attr"`
	ModuleInfo           *gsdmlModuleInfo           `xml:"ModuleInfo,omitempty"`
	VirtualSubmoduleList *gsdmlVirtualSubmoduleList `xml:"VirtualSubmoduleList,omitempty"`
}

type gsdmlVirtualSubmoduleList struct {
	VirtualSubmoduleItem []gsdmlVirtualSubmoduleItem `xml:"VirtualSubmoduleItem"`
}

type gsdmlVirtualSubmoduleItem struct {
	ID                   string           `xml:"ID,attr"`
	SubmoduleIdentNumber string           `xml:"SubmoduleIdentNumber,attr"`
	FixedInSubslots      string           `xml:"FixedInSubslots,attr,omitempty"`
	MayIssueProcessAlarm string           `xml:"MayIssueProcessAlarm,attr,omitempty"`
	IOData               *gsdmlIOData     `xml:"IOData,omitempty"`
	ModuleInfo           *gsdmlModuleInfo `xml:"ModuleInfo,omitempty"`
}

type gsdmlIOData struct {
	Input  *gsdmlIODataDir `xml:"Input,omitempty"`
	Output *gsdmlIODataDir `xml:"Output,omitempty"`
}

type gsdmlIODataDir struct {
	DataItem []gsdmlDataItem `xml:"DataItem"`
}

type gsdmlDataItem struct {
	DataType string `xml:"DataType,attr"`
	Length   string `xml:"Length,attr,omitempty"`
	TextId   string `xml:"TextId,attr"`
}

// ExternalTextList (GSDML standard text format)
type gsdmlExternalTextList struct {
	PrimaryLanguage gsdmlPrimaryLanguage `xml:"PrimaryLanguage"`
}

type gsdmlPrimaryLanguage struct {
	Text []gsdmlTextEntry `xml:"Text"`
}

type gsdmlTextEntry struct {
	TextId string `xml:"TextId,attr"`
	Value  string `xml:"Value,attr"`
}

func buildGSDMLDocument(cfg *ProfinetConfig) gsdmlISO15745Profile {
	var texts []gsdmlTextEntry
	addText := func(id, value string) {
		texts = append(texts, gsdmlTextEntry{TextId: id, Value: value})
	}

	addText("IDT_INFO_Device", cfg.DeviceName)
	addText("IDT_NAME_DAP", "DAP")
	addText("IDT_INFO_DAP", "Device Access Point")
	addText("IDT_NAME_Interface", "Interface")
	addText("IDT_NAME_Port1", "Port 1")

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

	// Build module items and refs
	var moduleItems []gsdmlModuleItem
	var moduleRefs []gsdmlModuleItemRef

	for _, slot := range cfg.Slots {
		if slot.SlotNumber == 0 {
			continue // DAP handled separately
		}

		moduleID := fmt.Sprintf("IDM_Mod%d", slot.SlotNumber)
		moduleNameID := fmt.Sprintf("IDT_NAME_Mod%d", slot.SlotNumber)
		moduleInfoID := fmt.Sprintf("IDT_INFO_Mod%d", slot.SlotNumber)
		addText(moduleNameID, fmt.Sprintf("Module Slot %d", slot.SlotNumber))
		addText(moduleInfoID, fmt.Sprintf("Module Slot %d", slot.SlotNumber))

		var virtualSubs []gsdmlVirtualSubmoduleItem
		for _, sub := range slot.Subslots {
			subID := fmt.Sprintf("IDS_Mod%d_Sub%d", slot.SlotNumber, sub.SubslotNumber)
			subNameID := fmt.Sprintf("IDT_NAME_Mod%d_Sub%d", slot.SlotNumber, sub.SubslotNumber)
			subInfoID := fmt.Sprintf("IDT_INFO_Mod%d_Sub%d", slot.SlotNumber, sub.SubslotNumber)
			addText(subNameID, fmt.Sprintf("Slot %d Subslot %d", slot.SlotNumber, sub.SubslotNumber))
			addText(subInfoID, fmt.Sprintf("Slot %d Subslot %d", slot.SlotNumber, sub.SubslotNumber))

			ioData := buildIOData(slot.SlotNumber, sub, addText)

			virtualSubs = append(virtualSubs, gsdmlVirtualSubmoduleItem{
				ID:                   subID,
				SubmoduleIdentNumber: fmt.Sprintf("0x%08X", sub.SubmoduleIdentNo),
				FixedInSubslots:      fmt.Sprintf("%d", sub.SubslotNumber),
				MayIssueProcessAlarm: "true",
				IOData:               ioData,
				ModuleInfo: &gsdmlModuleInfo{
					Name:     &gsdmlTextRef{TextId: subNameID},
					InfoText: &gsdmlTextRef{TextId: subInfoID},
				},
			})
		}

		moduleItems = append(moduleItems, gsdmlModuleItem{
			ID:                moduleID,
			ModuleIdentNumber: fmt.Sprintf("0x%08X", slot.ModuleIdentNo),
			ModuleInfo: &gsdmlModuleInfo{
				Name:     &gsdmlTextRef{TextId: moduleNameID},
				InfoText: &gsdmlTextRef{TextId: moduleInfoID},
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

	profile := gsdmlISO15745Profile{
		Xmlns:          "http://www.profibus.com/GSDML/2003/11/DeviceProfile",
		XSI:            "http://www.w3.org/2001/XMLSchema-instance",
		SchemaLocation: "http://www.profibus.com/GSDML/2003/11/DeviceProfile gsdml-v2.35.xsd",
		ProfileHeader: gsdmlProfileHeader{
			ProfileIdentification: "PROFINET Device Profile",
			ProfileRevision:       "1.00",
			ProfileName:           "Device Profile for PROFINET Devices",
			ProfileSource:         "PROFIBUS Nutzerorganisation e. V. (PNO)",
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
				Family: gsdmlFamily{
					MainFamily:    "I/O",
					ProductFamily: "Gateway",
				},
			},
			ApplicationProcess: gsdmlApplicationProcess{
				DeviceAccessPointList: gsdmlDAPList{
					DeviceAccessPointItem: []gsdmlDAPItem{
						{
							ID:                     "IDD_1",
							PNIO_Version:           "V2.4",
							PhysicalSlots:          fmt.Sprintf("0..%d", maxSlot),
							ModuleIdentNumber:      "0x00010000",
							MinDeviceInterval:      fmt.Sprintf("%d", minInterval),
							DNS_CompatibleName:     cfg.StationName,
							FixedInSlots:           "0",
							ObjectUUID_LocalIndex:  "1",
							DeviceAccessSupported:  "true",
							NumberOfDeviceAccessAR: "1",
							MultipleWriteSupported: "true",
							ModuleInfo: &gsdmlModuleInfo{
								Name:       &gsdmlTextRef{TextId: "IDT_NAME_DAP"},
								InfoText:   &gsdmlTextRef{TextId: "IDT_INFO_DAP"},
								VendorName: &gsdmlValue{Value: "JoyAutomation"},
								OrderNumber: &gsdmlValue{Value: "TENTACLE-PN"},
							},
							IOConfigData: &gsdmlIOConfigData{
								MaxInputLength:  "244",
								MaxOutputLength: "244",
							},
							UseableModules: &gsdmlUseableModules{
								ModuleItemRef: moduleRefs,
							},
							VirtualSubmoduleList: &gsdmlVirtualSubmoduleList{
								VirtualSubmoduleItem: []gsdmlVirtualSubmoduleItem{
									{
										ID:                   "IDS_DAP",
										SubmoduleIdentNumber: "0x00000001",
										MayIssueProcessAlarm: "false",
										IOData:               nil,
										ModuleInfo: &gsdmlModuleInfo{
											Name:     &gsdmlTextRef{TextId: "IDT_NAME_DAP"},
											InfoText: &gsdmlTextRef{TextId: "IDT_INFO_DAP"},
										},
									},
								},
							},
							SystemDefinedSubmoduleList: &gsdmlSystemSubmoduleList{
								InterfaceSubmoduleItem: []gsdmlInterfaceSubmodule{
									{
										ID:                   "IDS_I",
										SubmoduleIdentNumber: "0x00008000",
										SubslotNumber:        "32768",
										TextId:               "IDT_NAME_Interface",
										SupportedRT_Classes:  "RT_CLASS_1",
										SupportedProtocols:   "LLDP",
										ApplicationRelations: &gsdmlApplicationRelations{
											StartupMode: "Legacy;Advanced",
											TimingProperties: gsdmlTimingProperties{
												SendClock:      "32",
												ReductionRatio: "1 2 4 8 16 32 64 128 256 512",
											},
										},
									},
								},
								PortSubmoduleItem: []gsdmlPortSubmodule{
									{
										ID:                   "IDS_P1",
										SubmoduleIdentNumber: "0x00008001",
										SubslotNumber:        "32769",
										TextId:               "IDT_NAME_Port1",
										MaxPortRxDelay:       "350",
										MaxPortTxDelay:       "160",
										MAUTypeList: &gsdmlMAUTypeList{
											MAUTypeItem: []gsdmlMAUTypeItem{
												{Value: "16"},
											},
										},
									},
								},
							},
						},
					},
				},
				ModuleList: gsdmlModuleList{
					ModuleItem: moduleItems,
				},
				ExternalTextList: gsdmlExternalTextList{
					PrimaryLanguage: gsdmlPrimaryLanguage{
						Text: texts,
					},
				},
			},
		},
	}

	// If no user modules, ensure empty slices
	if len(moduleItems) == 0 {
		profile.ProfileBody.ApplicationProcess.DeviceAccessPointList.DeviceAccessPointItem[0].PhysicalSlots = "0"
		profile.ProfileBody.ApplicationProcess.DeviceAccessPointList.DeviceAccessPointItem[0].UseableModules = nil
	}

	return profile
}

func buildIOData(slotNum uint16, sub SubslotConfig, addText func(string, string)) *gsdmlIOData {
	ioData := &gsdmlIOData{}

	if sub.InputSize > 0 && (sub.Direction == DirectionInput || sub.Direction == DirectionInputOutput) {
		textId := fmt.Sprintf("IDT_IO_Slot%d_Sub%d_In", slotNum, sub.SubslotNumber)
		addText(textId, fmt.Sprintf("Input data slot %d subslot %d", slotNum, sub.SubslotNumber))
		ioData.Input = &gsdmlIODataDir{
			DataItem: []gsdmlDataItem{
				{
					DataType: "OctetString",
					Length:   fmt.Sprintf("%d", sub.InputSize),
					TextId:   textId,
				},
			},
		}
	}

	if sub.OutputSize > 0 && (sub.Direction == DirectionOutput || sub.Direction == DirectionInputOutput) {
		textId := fmt.Sprintf("IDT_IO_Slot%d_Sub%d_Out", slotNum, sub.SubslotNumber)
		addText(textId, fmt.Sprintf("Output data slot %d subslot %d", slotNum, sub.SubslotNumber))
		ioData.Output = &gsdmlIODataDir{
			DataItem: []gsdmlDataItem{
				{
					DataType: "OctetString",
					Length:   fmt.Sprintf("%d", sub.OutputSize),
					TextId:   textId,
				},
			},
		}
	}

	if ioData.Input == nil && ioData.Output == nil {
		return nil
	}
	return ioData
}
