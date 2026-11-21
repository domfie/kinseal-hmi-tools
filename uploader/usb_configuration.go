package main

import (
	"errors"
	"fmt"
)

var errNoActiveUSBConfiguration = errors.New("no active USB configuration")

type usbConfiguration struct {
	Value      int
	Source     string
	Interfaces []USBInterface
}

type usbConfigurationAccess struct {
	Active     func() (usbConfiguration, error)
	Advertised func() ([]usbConfiguration, error)
	Current    func() (int, error)
	Activate   func(int) error
}

// Native USB configuration selection is separate from KinSeal commands.
// It can be replayed against the observed libusb failure without hardware.
func prepareUSBConfiguration(access usbConfigurationAccess, configure bool) (usbConfiguration, error) {
	current, err := access.Active()
	if err == nil {
		current.Source = "active"
		return current, nil
	}
	if !errors.Is(err, errNoActiveUSBConfiguration) {
		return usbConfiguration{}, err
	}
	configs, err := access.Advertised()
	if err != nil {
		return usbConfiguration{}, err
	}
	if len(configs) != 1 || configs[0].Value <= 0 {
		return usbConfiguration{}, fmt.Errorf("aktive USB-Konfiguration fehlt; %d angebotene Konfigurationen, keine eindeutige Auswahl", len(configs))
	}
	selected := configs[0]
	selected.Source = "advertised-only"
	if !configure {
		return selected, nil
	}
	// Validate the known interface before any SET_CONFIGURATION request.
	compatible := false
	for _, i := range selected.Interfaces {
		if i.Number == 0 && i.Alternate == 0 && validateUSBInterface(i) == nil {
			compatible = true
		}
	}
	if !compatible {
		return usbConfiguration{}, fmt.Errorf("angebotene Konfiguration %d enthaelt kein passendes Interface 0 / Alt 0 mit 0x01/0x81", selected.Value)
	}
	value, err := access.Current()
	if err != nil {
		return usbConfiguration{}, err
	}
	if value != 0 {
		return usbConfiguration{}, fmt.Errorf("USB-Konfiguration %d ist aktiv, aber ihr Deskriptor fehlt; keine Umkonfiguration", value)
	}
	if err := access.Activate(selected.Value); err != nil {
		return usbConfiguration{}, err
	}
	confirmed, err := access.Active()
	if err != nil {
		return usbConfiguration{}, fmt.Errorf("USB-Konfiguration %d ausgewaehlt, aktive Konfiguration weiterhin nicht lesbar: %w", selected.Value, err)
	}
	if confirmed.Value != selected.Value {
		return usbConfiguration{}, fmt.Errorf("aktive USB-Konfiguration %d statt ausgewaehlter %d", confirmed.Value, selected.Value)
	}
	confirmed.Source = "selected-and-confirmed"
	return confirmed, nil
}
