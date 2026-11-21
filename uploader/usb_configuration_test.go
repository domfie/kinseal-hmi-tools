package main

import (
	"errors"
	"fmt"
	"testing"
)

func validConfiguration(value int) usbConfiguration {
	return usbConfiguration{Value: value, Interfaces: []USBInterface{{Number: 0, Alternate: 0,
		Pipes: []USBPipe{{Address: 1, Type: 3, MaxPacket: 256}, {Address: 0x81, Type: 3, MaxPacket: 256}}}}}
}

func TestUnconfiguredMacDeviceCanReachInterfaceValidation(t *testing.T) {
	active := 0
	selections := 0
	a := usbConfigurationAccess{
		Active: func() (usbConfiguration, error) {
			if active == 0 {
				return usbConfiguration{}, fmt.Errorf("%w: libusb_get_active_config_descriptor: LIBUSB_ERROR_NOT_FOUND (-5)", errNoActiveUSBConfiguration)
			}
			return validConfiguration(active), nil
		},
		Advertised: func() ([]usbConfiguration, error) { return []usbConfiguration{validConfiguration(2)}, nil },
		Current:    func() (int, error) { return active, nil },
		Activate:   func(value int) error { selections++; active = value; return nil },
	}
	c, err := prepareUSBConfiguration(a, true)
	if err != nil {
		t.Fatalf("probe still stops at the captured macOS failure: %v", err)
	}
	if active != 2 || c.Value != 2 || selections != 1 {
		t.Fatalf("must select advertised bConfigurationValue, not hard-code 1: active=%d result=%+v selections=%d", active, c, selections)
	}
	if err := validateUSBInterface(c.Interfaces[0]); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationDiagnosticsDoNotActivateOrOpenDevice(t *testing.T) {
	a := usbConfigurationAccess{
		Active:     func() (usbConfiguration, error) { return usbConfiguration{}, errNoActiveUSBConfiguration },
		Advertised: func() ([]usbConfiguration, error) { return []usbConfiguration{validConfiguration(1)}, nil },
		Current:    func() (int, error) { t.Fatal("diagnostics must not open/query a handle"); return 0, nil },
		Activate:   func(int) error { t.Fatal("diagnostics must not activate"); return nil },
	}
	c, err := prepareUSBConfiguration(a, false)
	if err != nil || c.Value != 1 || c.Source != "advertised-only" {
		t.Fatalf("missing read-only advertised descriptor: %+v, %v", c, err)
	}
}

func TestConfigurationSelectionRejectsAmbiguityAndFailures(t *testing.T) {
	permission := errors.New("USB access denied")
	for _, scenario := range []string{"multiple", "wrong-endpoints", "nonzero-current", "query-error", "set-error", "not-activated", "other-active-error"} {
		t.Run(scenario, func(t *testing.T) {
			c := validConfiguration(1)
			configs := []usbConfiguration{c}
			if scenario == "multiple" {
				configs = append(configs, validConfiguration(2))
			}
			if scenario == "wrong-endpoints" {
				configs[0].Interfaces[0].Pipes[0].Address = 2
			}
			setCalls := 0
			a := usbConfigurationAccess{
				Active: func() (usbConfiguration, error) {
					if scenario == "other-active-error" {
						return usbConfiguration{}, permission
					}
					return usbConfiguration{}, errNoActiveUSBConfiguration
				},
				Advertised: func() ([]usbConfiguration, error) { return configs, nil },
				Current: func() (int, error) {
					if scenario == "nonzero-current" {
						return 3, nil
					}
					if scenario == "query-error" {
						return 0, permission
					}
					return 0, nil
				},
				Activate: func(int) error {
					setCalls++
					if scenario == "set-error" {
						return permission
					}
					return nil
				},
			}
			if _, err := prepareUSBConfiguration(a, true); err == nil {
				t.Fatal("expected configuration failure")
			}
			wantCalls := 0
			if scenario == "set-error" || scenario == "not-activated" {
				wantCalls = 1
			}
			if setCalls != wantCalls {
				t.Fatalf("selection calls: got %d want %d", setCalls, wantCalls)
			}
		})
	}
}

func TestExistingActiveConfigurationIsNotChanged(t *testing.T) {
	a := usbConfigurationAccess{
		Active:     func() (usbConfiguration, error) { return validConfiguration(1), nil },
		Advertised: func() ([]usbConfiguration, error) { t.Fatal("must not inspect alternatives"); return nil, nil },
		Current:    func() (int, error) { t.Fatal("must not open for configuration selection"); return 0, nil },
		Activate:   func(int) error { t.Fatal("must not change an active configuration"); return nil },
	}
	c, err := prepareUSBConfiguration(a, true)
	if err != nil || c.Value != 1 || c.Source != "active" {
		t.Fatal(c, err)
	}
}
