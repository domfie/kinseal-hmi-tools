//go:build !windows && (!darwin || !cgo)

package main

import "fmt"

func discoverUSB(USBOptions) ([]Device, error) {
	return nil, fmt.Errorf("USB-Zugriff ist in diesem Build nur unter Windows verfügbar; inspect funktioniert auch hier")
}

func openUSB(USBOptions) (Transport, Device, error) {
	return nil, Device{}, fmt.Errorf("probe/upload benötigen den Windows-Build")
}

func inspectUSB(USBOptions) (USBDiagnostics, error) {
	return USBDiagnostics{}, fmt.Errorf("usb-info benötigt den Windows-Build")
}
