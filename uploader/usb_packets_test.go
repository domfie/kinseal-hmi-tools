package main

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestUSBRedirInterruptPayloadDelivery(t *testing.T) {
	// Models usbredirhost_interrupt_packet's rejection of OUT data larger than
	// max_packetsize, combined with QEMU's immediate guest-side success. A reported
	// byte count alone is therefore insufficient; assert all bytes reach the host.
	for _, size := range []int{32, 256, 257, 4998, 65536, 212104} {
		input := bytes.Repeat([]byte{0x12, 0x34, 0x56, 0x78}, (size+3)/4)[:size]
		var delivered []byte
		calls := 0
		n, err := writeUSBPackets(input, 256, func(b []byte) (int, error) {
			calls++
			if len(b) <= 256 {
				delivered = append(delivered, b...)
			}
			return len(b), nil
		})
		if err != nil || n != len(input) {
			t.Fatalf("size %d: %d, %v", size, n, err)
		}
		if !bytes.Equal(delivered, input) {
			t.Fatalf("size %d: guest reported %d bytes, host received %d; oversized interrupt OUT lost", size, n, len(delivered))
		}
		if calls != (size+255)/256 {
			t.Fatalf("size %d: packet count %d", size, calls)
		}
	}
}

func TestUSBPacketWriteStopsAtFirstFailure(t *testing.T) {
	failure := errors.New("transport failure")
	for _, test := range []struct {
		name string
		n    int
		err  error
		want error
	}{
		{"error", 0, failure, failure},
		{"partial", 7, nil, io.ErrShortWrite},
		{"partial-error", 7, failure, failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			n, err := writeUSBPackets(make([]byte, 1024), 256, func(b []byte) (int, error) {
				calls++
				if calls == 2 {
					return test.n, test.err
				}
				return len(b), nil
			})
			if calls != 2 || n != 256+test.n || !errors.Is(err, test.want) {
				t.Fatalf("calls=%d n=%d err=%v", calls, n, err)
			}
		})
	}
}

func TestBulkWritesRemainUnsplit(t *testing.T) {
	input := make([]byte, 212104)
	calls := 0
	n, err := writeUSBPackets(input, 0, func(b []byte) (int, error) { calls++; return len(b), nil })
	if err != nil || n != len(input) || calls != 1 {
		t.Fatalf("n=%d calls=%d err=%v", n, calls, err)
	}
}

func TestEmptyPacketWriteDoesNotSendZLP(t *testing.T) {
	n, err := writeUSBPackets(nil, 256, func([]byte) (int, error) { t.Fatal("unexpected ZLP"); return 0, nil })
	if n != 0 || err != nil {
		t.Fatal(n, err)
	}
}
