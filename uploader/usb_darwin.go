//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lusb-1.0
#include <libusb-1.0/libusb.h>

static const struct libusb_interface_descriptor *ks_interface(
    const struct libusb_config_descriptor *c, int i, int a) {
    return &c->interface[i].altsetting[a];
}
static int ks_alt_count(const struct libusb_config_descriptor *c, int i) {
    return c->interface[i].num_altsetting;
}
static const struct libusb_endpoint_descriptor *ks_endpoint(
    const struct libusb_interface_descriptor *i, int e) { return &i->endpoint[e]; }
*/
import "C"

import (
	"fmt"
	"runtime"
	"strings"
	"unsafe"
)

type darwinTransport struct {
	ctx           *C.libusb_context
	handle        *C.libusb_device_handle
	timeout       C.uint
	pipeType      uint32
	packetSize    int
	claimed       bool
	interfaces    []USBInterface
	configuration usbConfiguration
}

func darwinError(stage string, code C.int) error {
	return fmt.Errorf("%s: %s (%d)", stage, C.GoString(C.libusb_error_name(code)), int(code))
}

// Bus and physical port chain remain stable across normal device enumeration.
// The address is a fallback only when the backend cannot report physical ports.
func darwinDevice(dev *C.libusb_device, d C.struct_libusb_device_descriptor) Device {
	var ports [8]C.uint8_t
	n := C.libusb_get_port_numbers(dev, &ports[0], C.int(len(ports)))
	var path []string
	if n > 0 {
		for _, port := range ports[:int(n)] {
			path = append(path, fmt.Sprint(uint8(port)))
		}
	} else {
		path = []string{fmt.Sprintf("address-%d", uint8(C.libusb_get_device_address(dev)))}
	}
	id := fmt.Sprintf("libusb://bus/%d/port/%s/%04x:%04x", uint8(C.libusb_get_bus_number(dev)), strings.Join(path, "."), uint16(d.idVendor), uint16(d.idProduct))
	return Device{Instance: id, Paths: []string{id}, VID: uint16(d.idVendor), PID: uint16(d.idProduct),
		Description: fmt.Sprintf("USB %04X:%04X (macOS/libusb)", uint16(d.idVendor), uint16(d.idProduct)), Service: "libusb-1.0"}
}

func darwinList(ctx *C.libusb_context) (**C.libusb_device, []*C.libusb_device, error) {
	var list **C.libusb_device
	n := C.libusb_get_device_list(ctx, &list)
	if n < 0 {
		return nil, nil, darwinError("libusb_get_device_list", C.int(n))
	}
	return list, unsafe.Slice(list, int(n)), nil
}

func discoverUSB(o USBOptions) ([]Device, error) {
	var ctx *C.libusb_context
	if code := C.libusb_init(&ctx); code != 0 {
		return nil, darwinError("libusb_init", code)
	}
	defer C.libusb_exit(ctx)
	list, devices, err := darwinList(ctx)
	if err != nil {
		return nil, err
	}
	defer C.libusb_free_device_list(list, 1)
	var result []Device
	for _, dev := range devices {
		var d C.struct_libusb_device_descriptor
		if C.libusb_get_device_descriptor(dev, &d) == 0 && matchesDevice(uint16(d.idVendor), uint16(d.idProduct), o) {
			result = append(result, darwinDevice(dev, d))
		}
	}
	return result, nil
}

func darwinConfiguration(dev *C.libusb_device, index int) (usbConfiguration, error) {
	var config *C.struct_libusb_config_descriptor
	var code C.int
	if index < 0 {
		code = C.libusb_get_active_config_descriptor(dev, &config)
	} else {
		code = C.libusb_get_config_descriptor(dev, C.uint8_t(index), &config)
	}
	if code != 0 {
		if index < 0 && code == C.LIBUSB_ERROR_NOT_FOUND {
			return usbConfiguration{}, fmt.Errorf("%w: %v", errNoActiveUSBConfiguration, darwinError("libusb_get_active_config_descriptor", code))
		}
		return usbConfiguration{}, darwinError("libusb_get_config_descriptor", code)
	}
	defer C.libusb_free_config_descriptor(config)
	var result []USBInterface
	for n := 0; n < int(config.bNumInterfaces); n++ {
		for a := 0; a < int(C.ks_alt_count(config, C.int(n))); a++ {
			d := C.ks_interface(config, C.int(n), C.int(a))
			raw := [9]byte{byte(d.bLength), byte(d.bDescriptorType), byte(d.bInterfaceNumber), byte(d.bAlternateSetting), byte(d.bNumEndpoints), byte(d.bInterfaceClass), byte(d.bInterfaceSubClass), byte(d.bInterfaceProtocol), byte(d.iInterface)}
			i, err := readUSBInterface(byte(a), func(byte) ([9]byte, error) { return raw, nil }, func(_ byte, index byte) (USBPipe, error) {
				e := C.ks_endpoint(d, C.int(index))
				return USBPipe{Address: byte(e.bEndpointAddress), Type: uint32(e.bmAttributes & 3), MaxPacket: uint16(e.wMaxPacketSize), Interval: byte(e.bInterval)}, nil
			})
			if err != nil {
				return usbConfiguration{}, err
			}
			result = append(result, i)
		}
	}
	return usbConfiguration{Value: int(config.bConfigurationValue), Interfaces: result}, nil
}

func openDarwinUSB(o USBOptions, claim bool) (*darwinTransport, Device, error) {
	var device Device
	if o.GUID != "" {
		return nil, device, fmt.Errorf("--guid gilt nur fuer Windows; macOS verwendet --path aus list")
	}
	t := &darwinTransport{timeout: C.uint(o.TimeoutMS)}
	if code := C.libusb_init(&t.ctx); code != 0 {
		return nil, device, darwinError("libusb_init", code)
	}
	keep := false
	defer func() {
		if !keep {
			t.Close()
		}
	}()
	list, devices, err := darwinList(t.ctx)
	if err != nil {
		return nil, device, err
	}
	defer C.libusb_free_device_list(list, 1)
	var selected *C.libusb_device
	for _, dev := range devices {
		var d C.struct_libusb_device_descriptor
		if C.libusb_get_device_descriptor(dev, &d) != 0 || !matchesDevice(uint16(d.idVendor), uint16(d.idProduct), o) {
			continue
		}
		candidate := darwinDevice(dev, d)
		if o.Path != "" && o.Path != candidate.Instance {
			continue
		}
		if selected != nil {
			return nil, device, fmt.Errorf("mehrere passende Displays; --path aus list angeben")
		}
		selected, device = dev, candidate
	}
	if selected == nil {
		return nil, device, fmt.Errorf("kein passendes USB-Display; USB an macOS statt an UTM durchreichen")
	}
	t.configuration, err = prepareUSBConfiguration(usbConfigurationAccess{
		Active: func() (usbConfiguration, error) { return darwinConfiguration(selected, -1) },
		Advertised: func() ([]usbConfiguration, error) {
			var d C.struct_libusb_device_descriptor
			if code := C.libusb_get_device_descriptor(selected, &d); code != 0 {
				return nil, darwinError("libusb_get_device_descriptor", code)
			}
			var configs []usbConfiguration
			for n := 0; n < int(d.bNumConfigurations); n++ {
				c, err := darwinConfiguration(selected, n)
				if err != nil {
					return nil, err
				}
				configs = append(configs, c)
			}
			return configs, nil
		},
		Current: func() (int, error) {
			if t.handle == nil {
				if code := C.libusb_open(selected, &t.handle); code != 0 {
					return 0, darwinError("libusb_open (UTM/Studio schliessen, macOS-USB-Zugriff pruefen)", code)
				}
			}
			var value C.int
			if code := C.libusb_get_configuration(t.handle, &value); code != 0 {
				return 0, darwinError("libusb_get_configuration", code)
			}
			return int(value), nil
		},
		Activate: func(value int) error {
			if code := C.libusb_set_configuration(t.handle, C.int(value)); code != 0 {
				return darwinError(fmt.Sprintf("libusb_set_configuration(%d)", value), code)
			}
			return nil
		},
	}, claim)
	if err != nil {
		return nil, device, err
	}
	t.interfaces = t.configuration.Interfaces
	if !claim {
		keep = true
		return t, device, nil
	}
	var current *USBInterface
	for n := range t.interfaces {
		i := &t.interfaces[n]
		if i.Number == 0 && i.Alternate == 0 {
			current = i
			break
		}
	}
	if current == nil {
		return nil, device, fmt.Errorf("Interface 0 / Alternate Setting 0 fehlt")
	}
	if err := validateUSBInterface(*current); err != nil {
		return nil, device, err
	}
	for _, pipe := range current.Pipes {
		if pipe.Address == 1 {
			t.pipeType = pipe.Type
			if pipe.Type == 3 {
				if pipe.MaxPacket == 0 {
					return nil, device, fmt.Errorf("Interrupt-OUT meldet Paketgroesse 0")
				}
				t.packetSize = int(pipe.MaxPacket)
			}
		}
	}
	if t.handle == nil {
		if code := C.libusb_open(selected, &t.handle); code != 0 {
			return nil, device, darwinError("libusb_open (UTM/Studio schliessen, macOS-USB-Zugriff pruefen)", code)
		}
	}
	// Capture is opt-in: Darwin/libusb may re-enumerate the device while detaching
	// a HID driver. On close, libusb reattaches the driver through auto-detach.
	if o.DetachKernelDriver {
		if code := C.libusb_set_auto_detach_kernel_driver(t.handle, 1); code != 0 {
			return nil, device, darwinError("libusb_set_auto_detach_kernel_driver", code)
		}
	}
	if code := C.libusb_claim_interface(t.handle, 0); code != 0 {
		return nil, device, darwinError("libusb_claim_interface (UTM trennen; fuer macOS-HID ggf. sudo und --detach-kernel-driver)", code)
	}
	t.claimed, keep = true, true
	return t, device, nil
}

func openUSB(o USBOptions) (Transport, Device, error) {
	return openDarwinUSB(o, true)
}

func inspectUSB(o USBOptions) (USBDiagnostics, error) {
	r := USBDiagnostics{Format: "kinseal-usb-info-v1", Version: version, Configurations: []USBConfiguration{}}
	t, device, err := openDarwinUSB(o, false)
	if err != nil {
		return r, err
	}
	r.Device, r.Interfaces = device, t.interfaces
	r.ConfigurationSource = t.configuration.Source
	value := t.configuration.Value
	r.ConfigurationValue = &value
	return r, t.Close()
}

func (t *darwinTransport) transfer(b []byte, endpoint byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if t.handle == nil {
		return 0, fmt.Errorf("USB-Verbindung geschlossen")
	}
	var n C.int
	var code C.int
	if t.pipeType == 3 {
		code = C.libusb_interrupt_transfer(t.handle, C.uchar(endpoint), (*C.uchar)(unsafe.Pointer(&b[0])), C.int(len(b)), &n, t.timeout)
	} else {
		code = C.libusb_bulk_transfer(t.handle, C.uchar(endpoint), (*C.uchar)(unsafe.Pointer(&b[0])), C.int(len(b)), &n, t.timeout)
	}
	runtime.KeepAlive(b)
	if code != 0 {
		return int(n), darwinError(fmt.Sprintf("USB-Endpunkt 0x%02X", endpoint), code)
	}
	return int(n), nil
}

func (t *darwinTransport) Write(b []byte) (int, error) {
	return writeUSBPackets(b, t.packetSize, func(packet []byte) (int, error) { return t.transfer(packet, 1) })
}
func (t *darwinTransport) Read(b []byte) (int, error) { return t.transfer(b, 0x81) }
func (t *darwinTransport) Close() error {
	var err error
	if t.handle != nil {
		if t.claimed {
			if code := C.libusb_release_interface(t.handle, 0); code != 0 && code != C.LIBUSB_ERROR_NO_DEVICE {
				err = darwinError("libusb_release_interface", code)
			}
		}
		C.libusb_close(t.handle)
		t.handle = nil
	}
	if t.ctx != nil {
		C.libusb_exit(t.ctx)
		t.ctx = nil
	}
	return err
}
