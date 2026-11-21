package main

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// Frame 61 of the successful Studio capture, download.pcapng.
// Only the first three option bytes are assigned download flags; the remaining
// vendor bytes contain uninitialized stack data and must not be copied.
func TestStudioCaptureDownloadStartFlags(t *testing.T) {
	captured, err := hex.DecodeString("4b696e5365616c53747564696f4b6973733401010120006008b0fe8d1208779e")
	if err != nil {
		t.Fatal(err)
	}
	f := newFake()
	s := Session{IO: f}
	if err := s.upload(fixturePackage(t), f.identity); err != nil {
		t.Fatal(err)
	}
	for _, b := range f.writes {
		if len(b) == 32 && bytes.Equal(b[:18], captured[:18]) {
			if !bytes.Equal(b[:21], captured[:21]) {
				t.Fatalf("download flags differ from successful Studio trace: got %x, want %x", b[18:21], captured[18:21])
			}
			return
		}
	}
	t.Fatal("download start not sent")
}

// The successful eight-page Studio upload uses block number 00 00 in frame 79,
// rather than encoding the page count (08) in the ControlPara command.
func TestStudioCaptureControlParaBlockNumber(t *testing.T) {
	files := fixtureFiles()
	files["sheetpara.dat"] = make([]byte, 8*72)
	files["controlpara.dat"] = []byte{1, 2, 3}
	files["controlpixel.dat"] = []byte{4, 5, 6}
	p, err := preparePackage(files)
	if err != nil {
		t.Fatal(err)
	}
	if p.PageCount != 8 {
		t.Fatal("fixture must contain eight pages")
	}
	f := newFake()
	s := Session{IO: f}
	if err := s.upload(p, f.identity); err != nil {
		t.Fatal(err)
	}
	for _, b := range f.writes {
		if len(b) == 32 && bytes.Equal(b[:17], []byte("KinSealStudioKiss")) && b[17] == 0x0b {
			if !bytes.Equal(b[18:20], []byte{0, 0}) {
				t.Fatalf("ControlPara differs from Studio frame 79: got %x, want 0000", b[18:20])
			}
			return
		}
	}
	t.Fatal("ControlPara command missing")
}
