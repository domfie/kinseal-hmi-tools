//go:build windows && (386 || arm64 || amd64)

package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Windows' own ARM64 system libraries: no libusb DLL, CGo or foreign-ISA code.
var (
	setup              = systemDLL("setupapi.dll")
	getClassDevs       = setup.NewProc("SetupDiGetClassDevsW")
	enumDeviceInfo     = setup.NewProc("SetupDiEnumDeviceInfo")
	getProperty        = setup.NewProc("SetupDiGetDeviceRegistryPropertyW")
	getInstanceID      = setup.NewProc("SetupDiGetDeviceInstanceIdW")
	openDevKey         = setup.NewProc("SetupDiOpenDevRegKey")
	enumInterface      = setup.NewProc("SetupDiEnumDeviceInterfaces")
	getInterfaceDetail = setup.NewProc("SetupDiGetDeviceInterfaceDetailW")
	destroyInfo        = setup.NewProc("SetupDiDestroyDeviceInfoList")
	advapi             = systemDLL("advapi32.dll")
	queryValue         = advapi.NewProc("RegQueryValueExW")
	openKey            = advapi.NewProc("RegOpenKeyExW")
	closeKey           = advapi.NewProc("RegCloseKey")
	winusb             = systemDLL("winusb.dll")
	usbInitialize      = winusb.NewProc("WinUsb_Initialize")
	usbFree            = winusb.NewProc("WinUsb_Free")
	usbQueryInterface  = winusb.NewProc("WinUsb_QueryInterfaceSettings")
	usbQueryPipe       = winusb.NewProc("WinUsb_QueryPipe")
	usbGetDescriptor   = winusb.NewProc("WinUsb_GetDescriptor")
	usbGetAssociated   = winusb.NewProc("WinUsb_GetAssociatedInterface")
	usbSetPolicy       = winusb.NewProc("WinUsb_SetPipePolicy")
	usbRead            = winusb.NewProc("WinUsb_ReadPipe")
	usbWrite           = winusb.NewProc("WinUsb_WritePipe")
)

func systemDLL(name string) *syscall.LazyDLL {
	// kernel32 is an OS KnownDLL, already loaded by the Go runtime. Resolve all
	// other DLLs by absolute System32 path to avoid loading a neighboring DLL.
	getDirectory := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemDirectoryW")
	b := make([]uint16, 32768)
	n, _, err := getDirectory.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	if n == 0 || n >= uintptr(len(b)) {
		panic(fmt.Sprintf("GetSystemDirectoryW: %v", err))
	}
	return syscall.NewLazyDLL(filepath.Join(syscall.UTF16ToString(b[:n]), name))
}

type guid struct {
	A    uint32
	B, C uint16
	D    [8]byte
}
type deviceInfo struct {
	Size     uint32
	Class    guid
	DevInst  uint32
	Reserved uintptr
}
type interfaceInfo struct {
	Size     uint32
	Class    guid
	Flags    uint32
	Reserved uintptr
}
type pipeInfo struct {
	Type      uint32
	ID        byte
	MaxPacket uint16
	Interval  byte
}

// These SetupAPI structures are 28 bytes on x86 and 32 on 64-bit Windows.
// A layout drift must
// fail at compile time, not corrupt a Windows API call on the user's machine.
const setupInfoSize = 24 + unsafe.Sizeof(uintptr(0))

var _ [setupInfoSize - unsafe.Sizeof(deviceInfo{})]byte
var _ [unsafe.Sizeof(deviceInfo{}) - setupInfoSize]byte
var _ [setupInfoSize - unsafe.Sizeof(interfaceInfo{})]byte
var _ [unsafe.Sizeof(interfaceInfo{}) - setupInfoSize]byte
var _ [12 - unsafe.Sizeof(pipeInfo{})]byte
var _ [unsafe.Sizeof(pipeInfo{}) - 12]byte
var _ [4 - unsafe.Offsetof(pipeInfo{}.ID)]byte
var _ [unsafe.Offsetof(pipeInfo{}.ID) - 4]byte

func parseGUID(s string) (guid, error) {
	var g guid
	s = strings.Trim(s, "{}")
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return g, fmt.Errorf("ungültige GUID %q", s)
	}
	n, err := strconv.ParseUint(s[:8], 16, 32)
	if err != nil {
		return g, err
	}
	g.A = uint32(n)
	n, err = strconv.ParseUint(s[9:13], 16, 16)
	if err != nil {
		return g, err
	}
	g.B = uint16(n)
	n, err = strconv.ParseUint(s[14:18], 16, 16)
	if err != nil {
		return g, err
	}
	g.C = uint16(n)
	tail := s[19:23] + s[24:]
	for i := range g.D {
		n, err = strconv.ParseUint(tail[i*2:i*2+2], 16, 8)
		if err != nil {
			return g, err
		}
		g.D[i] = byte(n)
	}
	return g, nil
}

func utf16Strings(b []byte) []string {
	var result []string
	var current []uint16
	for i := 0; i+1 < len(b); i += 2 {
		u := binary.LittleEndian.Uint16(b[i:])
		if u == 0 {
			if len(current) > 0 {
				result = append(result, syscall.UTF16ToString(current))
				current = nil
			}
		} else {
			current = append(current, u)
		}
	}
	if len(current) > 0 {
		result = append(result, syscall.UTF16ToString(current))
	}
	return result
}

func deviceProperty(h uintptr, d *deviceInfo, prop uintptr) string {
	b := make([]byte, 16384)
	var kind, required uint32
	ok, _, _ := getProperty.Call(h, uintptr(unsafe.Pointer(d)), prop, uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&required)))
	if ok == 0 || required > uint32(len(b)) {
		return ""
	}
	return strings.Join(utf16Strings(b[:required]), "; ")
}

func registryGUIDs(h uintptr, d *deviceInfo) []string {
	key, _, _ := openDevKey.Call(h, uintptr(unsafe.Pointer(d)), 1, 0, 1, 0x20019) // GLOBAL, DIREG_DEV, KEY_READ
	if key == ^uintptr(0) {
		return nil
	}
	defer closeKey.Call(key)
	keys := []uintptr{key}
	// Manual WinUSB installations and some Microsoft OS descriptors keep
	// DeviceInterfaceGUIDs in the Device Parameters subkey rather than the root.
	parameters, _ := syscall.UTF16PtrFromString("Device Parameters")
	var child uintptr
	status, _, _ := openKey.Call(key, uintptr(unsafe.Pointer(parameters)), 0, 0x20019, uintptr(unsafe.Pointer(&child)))
	if status == 0 {
		defer closeKey.Call(child)
		keys = append(keys, child)
	}
	var found []string
	for _, registryKey := range keys {
		for _, name := range []string{"DeviceInterfaceGUIDs", "DeviceInterfaceGUID"} {
			p, _ := syscall.UTF16PtrFromString(name)
			b := make([]byte, 16384)
			var kind uint32
			length := uint32(len(b))
			status, _, _ := queryValue.Call(registryKey, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&length)))
			if status == 0 && length <= uint32(len(b)) && (kind == 1 || kind == 7) {
				found = append(found, utf16Strings(b[:length])...)
			}
		}
	}
	return found
}

func hardwareVIDPID(s string) (uint16, uint16, bool) {
	s = strings.ToLower(s)
	vi, pi := strings.Index(s, "vid_"), strings.Index(s, "pid_")
	if vi < 0 || pi < 0 || vi+8 > len(s) || pi+8 > len(s) {
		return 0, 0, false
	}
	v, e1 := strconv.ParseUint(s[vi+4:vi+8], 16, 16)
	p, e2 := strconv.ParseUint(s[pi+4:pi+8], 16, 16)
	return uint16(v), uint16(p), e1 == nil && e2 == nil
}

func interfacePaths(guidText string) ([]string, error) {
	g, err := parseGUID(guidText)
	if err != nil {
		return nil, err
	}
	h, _, err := getClassDevs.Call(uintptr(unsafe.Pointer(&g)), 0, 0, 0x12) // PRESENT | DEVICEINTERFACE
	if h == ^uintptr(0) {
		return nil, fmt.Errorf("SetupDiGetClassDevs(%s): %w", guidText, err)
	}
	defer destroyInfo.Call(h)
	var paths []string
	for i := uint32(0); ; i++ {
		d := interfaceInfo{}
		d.Size = uint32(unsafe.Sizeof(d))
		ok, _, err := enumInterface.Call(h, 0, uintptr(unsafe.Pointer(&g)), uintptr(i), uintptr(unsafe.Pointer(&d)))
		if ok == 0 {
			if err == syscall.Errno(259) { // ERROR_NO_MORE_ITEMS
				break
			}
			return nil, fmt.Errorf("SetupDiEnumDeviceInterfaces: %w", err)
		}
		var required uint32
		ok, _, err = getInterfaceDetail.Call(h, uintptr(unsafe.Pointer(&d)), 0, 0, uintptr(unsafe.Pointer(&required)), 0)
		if ok == 0 && err != syscall.ERROR_INSUFFICIENT_BUFFER {
			return nil, fmt.Errorf("Interfacegröße: %w", err)
		}
		if required < 8 || required > 65536 {
			return nil, fmt.Errorf("ungültige Interfacegröße %d", required)
		}
		b := make([]byte, required)
		detailSize := uint32(6) // Unicode DETAIL_DATA_W is 6 on x86, 8 on ARM64/AMD64.
		if unsafe.Sizeof(uintptr(0)) == 8 {
			detailSize = 8
		}
		binary.LittleEndian.PutUint32(b, detailSize) // DevicePath starts at offset 4 on either ABI.
		ok, _, err = getInterfaceDetail.Call(h, uintptr(unsafe.Pointer(&d)), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&required)), 0)
		if ok == 0 {
			return nil, fmt.Errorf("Interfacepfad: %w", err)
		}
		parts := utf16Strings(b[4:])
		if len(parts) > 0 {
			paths = append(paths, parts[0])
		}
	}
	return paths, nil
}

func discoverUSB(o USBOptions) ([]Device, error) {
	if err := setup.Load(); err != nil {
		return nil, err
	}
	h, _, err := getClassDevs.Call(0, 0, 0, 6) // PRESENT | ALLCLASSES, enumerate devnodes read-only.
	if h == ^uintptr(0) {
		return nil, fmt.Errorf("SetupDiGetClassDevs: %w", err)
	}
	defer destroyInfo.Call(h)
	var devices []Device
	for i := uint32(0); ; i++ {
		d := deviceInfo{}
		d.Size = uint32(unsafe.Sizeof(d))
		ok, _, err := enumDeviceInfo.Call(h, uintptr(i), uintptr(unsafe.Pointer(&d)))
		if ok == 0 {
			if err == syscall.Errno(259) { // ERROR_NO_MORE_ITEMS
				break
			}
			return nil, fmt.Errorf("SetupDiEnumDeviceInfo: %w", err)
		}
		vid, pid, valid := hardwareVIDPID(deviceProperty(h, &d, 1)) // SPDRP_HARDWAREID
		if !valid || !matchesDevice(vid, pid, o) {
			continue
		}
		dev := Device{VID: vid, PID: pid, Description: deviceProperty(h, &d, 0), Service: deviceProperty(h, &d, 4)}
		id := make([]uint16, 2048)
		var required uint32
		ok, _, _ = getInstanceID.Call(h, uintptr(unsafe.Pointer(&d)), uintptr(unsafe.Pointer(&id[0])), uintptr(len(id)), uintptr(unsafe.Pointer(&required)))
		if ok != 0 {
			dev.Instance = syscall.UTF16ToString(id)
		}
		dev.InterfaceGUIDs = registryGUIDs(h, &d)
		if o.GUID != "" {
			dev.InterfaceGUIDs = append(dev.InterfaceGUIDs, o.GUID)
		}
		// Standard USB interface provides a diagnostic fallback. Actual WinUSB
		// interfaces normally come from the device's DeviceInterfaceGUIDs value.
		dev.InterfaceGUIDs = append(dev.InterfaceGUIDs, "{A5DCBF10-6530-11D2-901F-00C04FB951ED}")
		seen := map[string]bool{}
		for _, g := range dev.InterfaceGUIDs {
			paths, e := interfacePaths(g)
			if e != nil {
				dev.DiscoveryErrors = append(dev.DiscoveryErrors, e.Error())
				continue
			}
			for _, p := range paths {
				// The USB interface path contains the complete instance component.
				instancePath := strings.ReplaceAll(strings.ToLower(dev.Instance), "\\", "#")
				if instancePath != "" && strings.Contains(strings.ToLower(p), instancePath+"#") && !seen[strings.ToLower(p)] {
					seen[strings.ToLower(p)] = true
					dev.Paths = append(dev.Paths, p)
				}
			}
		}
		devices = append(devices, dev)
	}
	return devices, nil
}

type winTransport struct {
	writePacketSize int // wMaxPacketSize for interrupt OUT; zero for bulk
	file            syscall.Handle
	handle          uintptr
}

func openUSB(o USBOptions) (Transport, Device, error) {
	return openUSBMode(o, true)
}

func openUSBMode(o USBOptions, configure bool) (*winTransport, Device, error) {
	devices, err := discoverUSB(o)
	if err != nil {
		return nil, Device{}, err
	}
	var candidates []Device
	for _, d := range devices {
		if o.Path != "" {
			found := false
			for _, p := range d.Paths {
				if strings.EqualFold(p, o.Path) {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		candidates = append(candidates, d)
	}
	if len(candidates) != 1 {
		return nil, Device{}, fmt.Errorf("%d passende Geräte: list verwenden; mit --vid/--pid und bei Bedarf --path genau eines auswählen", len(candidates))
	}
	d := candidates[0]
	if !strings.EqualFold(d.Service, "WinUSB") {
		return nil, d, fmt.Errorf("Gerät nutzt Treiberdienst %q statt WinUSB; die EXE ersetzt nur den alten Usermode-Uploader. ARM64-WinUSB-Bindung ist weiterhin erforderlich", d.Service)
	}
	if err := winusb.Load(); err != nil {
		return nil, d, err
	}
	paths := d.Paths
	if o.Path != "" {
		paths = []string{o.Path}
	}
	var failures []string
	for _, path := range paths {
		t, err := openWinUSBPath(path, o.TimeoutMS, configure)
		if err == nil {
			d.Paths = []string{path}
			return t, d, nil
		}
		failures = append(failures, path+": "+err.Error())
	}
	return nil, d, fmt.Errorf("kein WinUSB-Zugriff: %s", strings.Join(failures, "; "))
}

func openWinUSBPath(path string, timeout uint32, configure bool) (*winTransport, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	f, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, fmt.Errorf("CreateFile: %w", err)
	}
	t := &winTransport{file: f}
	ok, _, err := usbInitialize.Call(uintptr(f), uintptr(unsafe.Pointer(&t.handle)))
	if ok == 0 {
		syscall.CloseHandle(f)
		return nil, fmt.Errorf("WinUsb_Initialize: %w", err)
	}
	if configure {
		if err := t.configure(timeout); err != nil {
			t.Close()
			return nil, err
		}
	}
	return t, nil
}

func inspectUSB(o USBOptions) (USBDiagnostics, error) {
	r := USBDiagnostics{Format: "kinseal-usb-info-v1", Version: version, Interfaces: []USBInterface{}, Configurations: []USBConfiguration{}}
	t, d, err := openUSBMode(o, false)
	if err != nil {
		return r, err
	}
	r.Device = d
	device, err := t.descriptor(1, 0, 18)
	count := 1
	if err != nil {
		r.Errors = append(r.Errors, err.Error())
	} else {
		r.DeviceDescriptor = hex.EncodeToString(device)
		if len(device) == 18 && device[0] == 18 && device[1] == 1 {
			count = int(device[17])
			if count > 16 {
				r.Errors = append(r.Errors, "mehr als 16 Konfigurationen: Ausgabe begrenzt")
				count = 16
			}
		} else {
			r.Errors = append(r.Errors, "ungültiger Gerätedeskriptor")
		}
	}
	for n := 0; n < count; n++ {
		b, e := t.descriptor(2, byte(n), 65535)
		c := USBConfiguration{Index: byte(n), Raw: hex.EncodeToString(b)}
		if e != nil {
			c.Error = e.Error()
		} else {
			c.Interfaces, e = parseUSBConfiguration(b)
			if e != nil {
				c.Error = e.Error()
			}
		}
		r.Configurations = append(r.Configurations, c)
	}
	collect := func(handle uintptr, index int) {
		target := &winTransport{handle: handle}
		for alt := 0; alt < 256; alt++ {
			i, e := target.describeInterface(byte(alt))
			if e != nil {
				if !errors.Is(e, syscall.Errno(259)) {
					r.Errors = append(r.Errors, fmt.Sprintf("Handle %d Alt-Index %d: %v", index, alt, e))
				}
				break
			}
			i.HandleIndex = index
			r.Interfaces = append(r.Interfaces, i)
		}
	}
	collect(t.handle, 0)
	for index := 0; index < 16; index++ {
		var handle uintptr
		ok, _, e := usbGetAssociated.Call(t.handle, uintptr(index), uintptr(unsafe.Pointer(&handle)))
		if ok == 0 {
			if !errors.Is(e, syscall.Errno(259)) {
				r.Errors = append(r.Errors, fmt.Sprintf("Associated Interface %d: %v", index, e))
			}
			break
		}
		collect(handle, index+1)
		ok, _, e = usbFree.Call(handle)
		if ok == 0 {
			r.Errors = append(r.Errors, fmt.Sprintf("Associated WinUsb_Free: %v", e))
		}
	}
	if e := t.Close(); e != nil {
		r.Errors = append(r.Errors, e.Error())
	}
	return r, nil
}

func (t *winTransport) descriptor(kind, index byte, capacity int) ([]byte, error) {
	b := make([]byte, capacity)
	var n uint32
	ok, _, err := usbGetDescriptor.Call(t.handle, uintptr(kind), uintptr(index), 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&n)))
	if n > uint32(len(b)) {
		return nil, fmt.Errorf("WinUsb_GetDescriptor meldet ungültige Länge %d", n)
	}
	b = b[:n]
	if ok == 0 {
		return b, fmt.Errorf("WinUsb_GetDescriptor Typ %d Index %d: %w", kind, index, err)
	}
	return b, nil
}

func (t *winTransport) configure(timeout uint32) error {
	i, err := t.describeInterface(0)
	if err != nil {
		return err
	}
	if err := validateUSBInterface(i); err != nil {
		return err
	}
	for _, p := range i.Pipes {
		if p.Address == 0x01 && p.Type == 3 {
			if p.MaxPacket == 0 {
				return fmt.Errorf("Interrupt-OUT meldet Paketgröße 0")
			}
			t.writePacketSize = int(p.MaxPacket)
		}
	}
	var ok uintptr
	for _, endpoint := range []byte{0x01, 0x81} {
		ok, _, err = usbSetPolicy.Call(t.handle, uintptr(endpoint), 3, 4, uintptr(unsafe.Pointer(&timeout)))
		if ok == 0 {
			return fmt.Errorf("PIPE_TRANSFER_TIMEOUT: %w", err)
		}
	}
	// Do not append ZLPs, discard excess replies, or silently clear stalls.
	for _, policy := range []struct {
		endpoint byte
		kind     uintptr
		value    byte
	}{{1, 1, 0}, {0x81, 4, 0}, {0x81, 5, 0}, {0x81, 6, 0}} {
		value := policy.value
		ok, _, err = usbSetPolicy.Call(t.handle, uintptr(policy.endpoint), policy.kind, 1, uintptr(unsafe.Pointer(&value)))
		if ok == 0 {
			return fmt.Errorf("Pipe-Policy %d: %w", policy.kind, err)
		}
	}
	return nil
}

func (t *winTransport) describeInterface(alt byte) (USBInterface, error) {
	return readUSBInterface(alt, func(alt byte) ([9]byte, error) {
		var desc [9]byte
		ok, _, err := usbQueryInterface.Call(t.handle, uintptr(alt), uintptr(unsafe.Pointer(&desc[0])))
		if ok == 0 {
			return desc, fmt.Errorf("WinUsb_QueryInterfaceSettings: %w", err)
		}
		return desc, nil
	}, func(alt, index byte) (USBPipe, error) {
		var pipe pipeInfo
		ok, _, err := usbQueryPipe.Call(t.handle, uintptr(alt), uintptr(index), uintptr(unsafe.Pointer(&pipe)))
		if ok == 0 {
			return USBPipe{}, fmt.Errorf("WinUsb_QueryPipe: %w", err)
		}
		raw := unsafe.Slice((*byte)(unsafe.Pointer(&pipe)), int(unsafe.Sizeof(pipe)))
		return USBPipe{Type: pipe.Type, Address: pipe.ID, MaxPacket: pipe.MaxPacket, Interval: pipe.Interval, Raw: fmt.Sprintf("%x", raw)}, nil
	})
}

func (t *winTransport) transfer(b []byte, proc *syscall.LazyProc) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	var transferred uint32
	endpoint := uintptr(1)
	if proc == usbRead {
		endpoint = 0x81
	}
	ok, _, err := proc.Call(t.handle, endpoint, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&transferred)), 0)
	runtime.KeepAlive(b)
	runtime.KeepAlive(t)
	if ok == 0 {
		return int(transferred), err
	}
	return int(transferred), nil
}

func (t *winTransport) Write(b []byte) (int, error) {
	return writeUSBPackets(b, t.writePacketSize, func(packet []byte) (int, error) {
		return t.transfer(packet, usbWrite)
	})
}
func (t *winTransport) Read(b []byte) (int, error) { return t.transfer(b, usbRead) }
func (t *winTransport) Close() error {
	var first error
	if t.handle != 0 {
		ok, _, err := usbFree.Call(t.handle)
		if ok == 0 {
			first = fmt.Errorf("WinUsb_Free: %w", err)
		}
		t.handle = 0
	}
	if t.file != syscall.InvalidHandle {
		err := syscall.CloseHandle(t.file)
		if first == nil {
			first = err
		}
		t.file = syscall.InvalidHandle
	}
	return first
}
