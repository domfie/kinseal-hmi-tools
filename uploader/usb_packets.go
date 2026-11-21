package main

import (
	"fmt"
	"io"
)

// writeUSBPackets bounds individual WinUSB OUT calls. QEMU usbredir reports
// immediate success for interrupt OUT, while usbredirhost rejects data larger
// than wMaxPacketSize. Never retry a partial write: delivery is then ambiguous.
// packetSize=0 keeps the existing bulk path unchanged.
func writeUSBPackets(b []byte, packetSize int, write func([]byte) (int, error)) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if packetSize <= 0 {
		return write(b)
	}
	total := 0
	for total < len(b) {
		end := total + packetSize
		if end > len(b) {
			end = len(b)
		}
		n, err := write(b[total:end])
		if n < 0 || n > end-total {
			return total, fmt.Errorf("ungültige USB-Schreiblänge %d", n)
		}
		total += n
		if err != nil {
			return total, err
		}
		if total != end {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
