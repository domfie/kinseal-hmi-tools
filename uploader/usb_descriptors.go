package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

type USBPipe struct {
	Index      byte   `json:"index"`
	Type       uint32 `json:"type"`
	Kind       string `json:"kind"`
	Address    byte   `json:"address"`
	AddressHex string `json:"address_hex"`
	MaxPacket  uint16 `json:"max_packet_size"`
	Interval   byte   `json:"interval"`
	Raw        string `json:"winusb_pipe_structure_hex,omitempty"`
}

type USBInterface struct {
	HandleIndex    int       `json:"handle_index"`
	AlternateIndex byte      `json:"alternate_index"`
	Number         byte      `json:"interface_number"`
	Alternate      byte      `json:"alternate_setting"`
	EndpointCount  byte      `json:"endpoint_count"`
	Class          byte      `json:"class"`
	Descriptor     string    `json:"descriptor_hex"`
	Pipes          []USBPipe `json:"pipes"`
	Errors         []string  `json:"errors,omitempty"`
}

type USBConfiguration struct {
	Index      byte           `json:"index"`
	Raw        string         `json:"descriptor_hex"`
	Error      string         `json:"error,omitempty"`
	Interfaces []USBInterface `json:"interfaces,omitempty"`
}

type USBDiagnostics struct {
	ConfigurationSource  string             `json:"configuration_source,omitempty"`
	ConfigurationValue   *int               `json:"configuration_value,omitempty"`
	Format               string             `json:"format"`
	Version              string             `json:"version"`
	Device               Device             `json:"device"`
	DeviceDescriptor     string             `json:"device_descriptor_hex,omitempty"`
	Interfaces           []USBInterface     `json:"winusb_interfaces"`
	Configurations       []USBConfiguration `json:"raw_configurations"`
	Errors               []string           `json:"errors,omitempty"`
	KinsealCommandsSent  bool               `json:"kinseal_commands_sent"`
	ProjectUploadStarted bool               `json:"project_upload_started"`
}

// Shared by the real WinUSB configure path and the read-only diagnostic command.
// Keeping this boundary injectable lets tests catch missing endpoint information
// without pretending to reproduce the user's unobserved Windows descriptors.
func readUSBInterface(alt byte, setting func(byte) ([9]byte, error), pipe func(byte, byte) (USBPipe, error)) (USBInterface, error) {
	d, err := setting(alt)
	if err != nil {
		return USBInterface{}, err
	}
	i := USBInterface{AlternateIndex: alt, Number: d[2], Alternate: d[3], EndpointCount: d[4], Class: d[5], Descriptor: hex.EncodeToString(d[:]), Pipes: []USBPipe{}}
	if d[0] != 9 || d[1] != 4 {
		return i, fmt.Errorf("ungültiger USB-Interfacedeskriptor %s", i.Descriptor)
	}
	for n := byte(0); n < i.EndpointCount; n++ {
		p, err := pipe(i.Alternate, n)
		if err != nil {
			i.Errors = append(i.Errors, fmt.Sprintf("Pipe %d: %v", n, err))
			continue
		}
		p.Index = n
		p.AddressHex = fmt.Sprintf("0x%02X", p.Address)
		switch p.Type {
		case 0:
			p.Kind = "control"
		case 1:
			p.Kind = "isochronous"
		case 2:
			p.Kind = "bulk"
		case 3:
			p.Kind = "interrupt"
		default:
			p.Kind = "unknown"
		}
		i.Pipes = append(i.Pipes, p)
	}
	return i, nil
}

func validateUSBInterface(i USBInterface) error {
	var pipes []string
	for _, p := range i.Pipes {
		pipes = append(pipes, fmt.Sprintf("0x%02X/%s(type=%d,maxpacket=%d)", p.Address, p.Kind, p.Type, p.MaxPacket))
	}
	detail := fmt.Sprintf("Interface %d, Alt %d, %d Endpunkte, Deskriptor %s; gefunden: [%s]", i.Number, i.Alternate, i.EndpointCount, i.Descriptor, strings.Join(pipes, ", "))
	if i.Number != 0 {
		return fmt.Errorf("unbelegtes Interface %d, erwartet 0", i.Number)
	}
	found := map[byte]uint32{}
	for _, p := range i.Pipes {
		if p.Type == 2 || p.Type == 3 {
			found[p.Address] = p.Type
		}
	}
	if len(i.Errors) > 0 {
		return fmt.Errorf("WinUsb_QueryPipe: %v; %s", i.Errors, detail)
	}
	if found[0x01] == 0 || found[0x81] == 0 || found[0x01] != found[0x81] {
		return fmt.Errorf("benötigtes Bulk- oder Interrupt-Endpunktpaar 0x01/0x81 fehlt; %s", detail)
	}
	return nil
}

func parseUSBConfiguration(b []byte) ([]USBInterface, error) {
	if len(b) < 9 || b[0] != 9 || b[1] != 2 || int(binary.LittleEndian.Uint16(b[2:])) != len(b) {
		return nil, fmt.Errorf("ungültiger oder unvollständiger Konfigurationsdeskriptor (%d Bytes)", len(b))
	}
	var interfaces []USBInterface
	for offset := 0; offset < len(b); {
		if len(b)-offset < 2 || b[offset] < 2 || int(b[offset]) > len(b)-offset {
			return nil, fmt.Errorf("ungültige Deskriptorlänge bei Offset %d", offset)
		}
		d := b[offset : offset+int(b[offset])]
		switch d[1] {
		case 4:
			if len(d) != 9 {
				return nil, fmt.Errorf("Interfacedeskriptor hat %d Bytes", len(d))
			}
			i := USBInterface{Number: d[2], Alternate: d[3], EndpointCount: d[4], Class: d[5], Descriptor: hex.EncodeToString(d), Pipes: []USBPipe{}}
			interfaces = append(interfaces, i)
		case 5:
			if len(d) < 7 || len(interfaces) == 0 {
				return nil, fmt.Errorf("Endpunkt ohne gültiges Interface")
			}
			i := &interfaces[len(interfaces)-1]
			kind := []string{"control", "isochronous", "bulk", "interrupt"}[d[3]&3]
			i.Pipes = append(i.Pipes, USBPipe{Index: byte(len(i.Pipes)), Type: uint32(d[3] & 3), Kind: kind, Address: d[2], AddressHex: fmt.Sprintf("0x%02X", d[2]), MaxPacket: binary.LittleEndian.Uint16(d[4:]), Interval: d[6]})
		}
		offset += len(d)
	}
	for _, i := range interfaces {
		if len(i.Pipes) != int(i.EndpointCount) {
			return interfaces, fmt.Errorf("Interface %d Alt %d meldet %d, enthält %d Endpunkte", i.Number, i.Alternate, i.EndpointCount, len(i.Pipes))
		}
	}
	return interfaces, nil
}
