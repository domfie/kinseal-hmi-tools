package main

type Device struct {
	Instance        string   `json:"instance"`
	Description     string   `json:"description"`
	Service         string   `json:"driver_service"`
	VID             uint16   `json:"vid"`
	PID             uint16   `json:"pid"`
	Paths           []string `json:"paths"`
	InterfaceGUIDs  []string `json:"interface_guids,omitempty"`
	DiscoveryErrors []string `json:"discovery_errors,omitempty"`
}

type USBOptions struct {
	DetachKernelDriver bool
	VID                uint16
	PID                uint16
	Path               string
	GUID               string
	All                bool
	TimeoutMS          uint32
}

func matchesDevice(vid, pid uint16, o USBOptions) bool {
	if o.VID != 0 || o.PID != 0 {
		return (o.VID == 0 || vid == o.VID) && (o.PID == 0 || pid == o.PID)
	}
	return o.All || (vid == 0x7365 && pid == 0x616c) || (vid == 0x5345 && pid == 0x1234)
}
