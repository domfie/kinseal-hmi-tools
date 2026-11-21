package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func TestMissingEndpointDiagnosticRetainsActualDescriptors(t *testing.T) {
	setting := func(byte) ([9]byte, error) { return [9]byte{9, 4, 0, 0, 2, 0xff, 0, 0, 0}, nil }
	pipe := func(_ byte, index byte) (USBPipe, error) {
		return USBPipe{Type: 2, Address: []byte{0x02, 0x82}[index], MaxPacket: 64}, nil
	}
	i, err := readUSBInterface(0, setting, pipe)
	if err != nil {
		t.Fatal(err)
	}
	err = validateUSBInterface(i)
	if err == nil {
		t.Fatal("unexpected endpoint pair accepted")
	}
	for _, detail := range []string{"0904000002ff000000", "0x02", "0x82", "bulk"} {
		if !strings.Contains(err.Error(), detail) {
			t.Fatalf("missing %q in endpoint diagnostic: %v", detail, err)
		}
	}
}

func TestEndpointValidationStillRejectsWrongTypeAndMissingPipe(t *testing.T) {
	for _, test := range []struct {
		name      string
		endpoints []USBPipe
		wantOK    bool
	}{
		{"bulk-pair", []USBPipe{{Type: 2, Address: 1}, {Type: 2, Address: 0x81}}, true},
		{"isochronous-pair", []USBPipe{{Type: 1, Address: 1}, {Type: 1, Address: 0x81}}, false},
		{"control-pair", []USBPipe{{Type: 0, Address: 1}, {Type: 0, Address: 0x81}}, false},
		{"interrupt-in", []USBPipe{{Type: 2, Address: 1}, {Type: 3, Address: 0x81}}, false},
		{"missing-in", []USBPipe{{Type: 2, Address: 1}}, false},
		{"empty-alt-zero", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			setting := func(byte) ([9]byte, error) { return [9]byte{9, 4, 0, 0, byte(len(test.endpoints)), 255, 0, 0, 0}, nil }
			pipe := func(_ byte, n byte) (USBPipe, error) { return test.endpoints[n], nil }
			i, err := readUSBInterface(0, setting, pipe)
			if err != nil {
				t.Fatal(err)
			}
			if (validateUSBInterface(i) == nil) != test.wantOK {
				t.Fatalf("validation verdict for %+v", i)
			}
		})
	}
}

func TestDiagnosticAlternateValueAndFailedPipeArePreserved(t *testing.T) {
	setting := func(index byte) ([9]byte, error) {
		if index != 1 {
			t.Fatal(index)
		}
		return [9]byte{9, 4, 0, 7, 2, 255, 0, 0, 0}, nil
	}
	pipe := func(alt, n byte) (USBPipe, error) {
		if alt != 7 {
			t.Fatalf("queried index instead of alternate value: %d", alt)
		}
		if n == 1 {
			return USBPipe{}, errors.New("test query failure")
		}
		return USBPipe{Type: 2, Address: 1}, nil
	}
	i, err := readUSBInterface(1, setting, pipe)
	if err != nil {
		t.Fatal(err)
	}
	if i.AlternateIndex != 1 || i.Alternate != 7 || len(i.Pipes) != 1 || len(i.Errors) != 1 {
		t.Fatalf("lost diagnostic: %+v", i)
	}
	if err := validateUSBInterface(i); err == nil || !strings.Contains(err.Error(), "test query failure") {
		t.Fatal(err)
	}
}

func rawConfigurationFixture() []byte {
	b := []byte{
		9, 2, 0, 0, 1, 1, 0, 0x80, 50,
		9, 4, 0, 0, 0, 255, 0, 0, 0,
		9, 4, 0, 7, 2, 255, 0, 0, 0,
		7, 5, 1, 2, 64, 0, 0,
		7, 5, 0x81, 2, 64, 0, 0,
	}
	binary.LittleEndian.PutUint16(b[2:], uint16(len(b)))
	return b
}

func TestRawConfigurationShowsAlternateEndpointPair(t *testing.T) {
	b := rawConfigurationFixture()
	before := append([]byte(nil), b...)
	i, err := parseUSBConfiguration(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(i) != 2 || i[0].EndpointCount != 0 || i[1].Alternate != 7 || i[1].Pipes[1].Address != 0x81 || i[1].Pipes[1].MaxPacket != 64 {
		t.Fatalf("wrong raw decode: %+v", i)
	}
	if validateUSBInterface(i[0]) == nil || validateUSBInterface(i[1]) != nil {
		t.Fatal("could not distinguish alternate layouts")
	}
	if !bytes.Equal(b, before) {
		t.Fatal("modified descriptor")
	}
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { return b[:len(b)-1] },
		func(b []byte) []byte { b[9] = 0; return b },
		func(b []byte) []byte { b[22] = 3; return b },
	} {
		bad := mutate(rawConfigurationFixture())
		if _, err := parseUSBConfiguration(bad); err == nil {
			t.Fatal("invalid descriptor accepted")
		}
	}
}

// Replays the interface and pipe values captured in the ARM64 0.2.1 output.txt.
func TestCapturedAMZ043InterruptInterface(t *testing.T) {
	setting := func(byte) ([9]byte, error) { return [9]byte{9, 4, 0, 0, 2, 3, 0, 0, 0}, nil }
	pipe := func(_ byte, index byte) (USBPipe, error) {
		return USBPipe{Type: 3, Address: []byte{0x81, 0x01}[index], MaxPacket: 256}, nil
	}
	i, err := readUSBInterface(0, setting, pipe)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateUSBInterface(i); err != nil {
		t.Fatalf("captured Kinseal interrupt pair rejected: %v", err)
	}
}
