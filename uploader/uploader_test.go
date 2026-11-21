package main

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// Synthetic data only: these fixtures are not valid HMI applications and must
// never be sent to hardware. They exercise the container and transport boundary.
func fixtureFiles() map[string][]byte {
	var source bytes.Buffer
	qstring := func(s string) {
		units := utf16.Encode([]rune(s))
		binary.Write(&source, binary.BigEndian, uint32(len(units)*2))
		for _, u := range units {
			binary.Write(&source, binary.BigEndian, u)
		}
	}
	qstring("KinSealStudio-V1.0.2")
	binary.Write(&source, binary.BigEndian, uint32(122))
	qstring("AMZ043W01")
	var snapshot bytes.Buffer
	binary.Write(&snapshot, binary.BigEndian, uint32(source.Len()))
	z := zlib.NewWriter(&snapshot)
	z.Write(source.Bytes())
	z.Close()
	m := map[string][]byte{}
	for _, name := range []string{"syspara.dat", "sheetpara.dat", "alarmpara.dat", "collect.dat", "addrpara.dat", "piclenpara.dat", "curve.dat", "sheetpixel.dat", "modbus_rtu_com1.bin"} {
		m[name] = []byte{1, 2, 3, 4, 5}
	}
	m["upload.knr"] = snapshot.Bytes()
	m["sheetpara.dat"] = make([]byte, 72)
	m["load.bin"] = append([]byte(nil), source.Bytes()...)
	m["usbload.bin"] = []byte{7, 6}
	m["alarmmuti.dat"] = nil
	m["marcovar.dat"] = nil
	return m
}

func fixturePackage(t *testing.T) *Package {
	t.Helper()
	p, err := preparePackage(fixtureFiles())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestArchiveAndWireFraming(t *testing.T) {
	p := fixturePackage(t)
	if p.SourceModel != "AMZ043W01" {
		t.Fatalf("model=%q", p.SourceModel)
	}
	var commands []byte
	for _, s := range p.Segments {
		commands = append(commands, s.Command)
		if s.Command == 0x1e {
			if int(binary.BigEndian.Uint32(s.Data)) != len(s.Data)-4 {
				t.Fatal("KNR wire length")
			}
		}
		if s.Command == 0x11 {
			if binary.LittleEndian.Uint32(s.Options) != 5 {
				t.Fatal("program length/options endian")
			}
		}
	}
	if !bytes.Equal(commands, []byte{0x0a, 0x0c, 0x0e, 0x16, 0x15, 0x19, 0x12, 0x14, 0x0d, 0x1e, 0x0f, 0x11}) {
		t.Fatalf("commands %x", commands)
	}
	// Literal wire oracle independent of frame(): 17 ASCII bytes + command + options + zeros.
	want, err := hex.DecodeString("4b696e5365616c53747564696f4b697373310200000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frame(0x31, []byte{2}), want) {
		t.Fatalf("frame=%x want=%x", frame(0x31, []byte{2}), want)
	}
}

func TestDynamicAndMacroOrdering(t *testing.T) {
	m := fixtureFiles()
	for _, n := range []string{"controlpara.dat", "controlpixel.dat", "marcovar.dat", "marcorun.dat"} {
		m[n] = []byte{1}
	}
	p, err := preparePackage(m)
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	for _, s := range p.Segments {
		got = append(got, s.Command)
	}
	want := []byte{0x0a, 0x0b, 0x0c, 0x36, 0x1d, 0x0e, 0x16, 0x15, 0x19, 0x12, 0x14, 0x0d, 0x1e, 0x10, 0x0f, 0x11}
	if !bytes.Equal(got, want) {
		t.Fatalf("%x != %x", got, want)
	}
}

func TestIncompleteOrUnmappedPackageRejected(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string][]byte)
	}{
		{"missing-system", func(m map[string][]byte) { delete(m, "syspara.dat") }},
		{"unmapped-message", func(m map[string][]byte) { m["simmessage.dat"] = []byte{1} }},
		{"unknown-segment", func(m map[string][]byte) { m["dataforward.dat"] = []byte{1} }},
		{"second-program", func(m map[string][]byte) { m["driver2.bin"] = []byte{1} }},
		{"half-control", func(m map[string][]byte) { m["controlpara.dat"] = []byte{1} }},
		{"half-macro", func(m map[string][]byte) { m["marcovar.dat"] = []byte{1} }},
		{"snapshot-bomb", func(m map[string][]byte) { m["upload.knr"] = []byte{0x7f, 0xff, 0xff, 0xff, 0x78, 0x9c} }},
		{"truncated-snapshot", func(m map[string][]byte) { m["upload.knr"] = m["upload.knr"][:10] }},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := fixtureFiles()
			test.mutate(m)
			if _, err := preparePackage(m); err == nil {
				t.Fatal("accepted unsupported package")
			}
		})
	}
}

func zipFixture(t *testing.T, mutate func(*zip.Writer)) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for n, v := range fixtureFiles() {
		f, err := w.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		f.Write(v)
	}
	if mutate != nil {
		mutate(w)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestZIPCRCAndNames(t *testing.T) {
	for _, name := range []string{"../escape.dat", "SYSPARA.DAT", "nested/file.dat", "C:syspara.dat"} {
		t.Run(name, func(t *testing.T) {
			data := zipFixture(t, func(w *zip.Writer) { f, _ := w.Create(name); f.Write([]byte{1}) })
			p := filepath.Join(t.TempDir(), "load.kns")
			os.WriteFile(p, data, 0600)
			if _, err := loadPackage(p); err == nil {
				t.Fatal("accepted unsafe/duplicate archive name")
			}
		})
	}
	data := zipFixture(t, nil)
	p := filepath.Join(t.TempDir(), "export.bin") // Signature, not extension, determines the supported container.
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPackage(p); err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.File {
		if f.Name == "syspara.dat" {
			off, _ := f.DataOffset()
			data[off] ^= 0xff
		}
	}
	os.WriteFile(p, data, 0600)
	if _, err := loadPackage(p); err == nil {
		t.Fatal("corrupt ZIP CRC accepted")
	}
	os.WriteFile(p, []byte("arbitrary vendor binary"), 0600)
	if _, err := loadPackage(p); err == nil {
		t.Fatal("raw binary treated as complete package")
	}
}

type fakeUSB struct {
	writes       [][]byte
	reads        int
	last         byte
	identity     Identity
	nack         byte
	shortPayload bool
	readError    byte
	ack          byte
	onWrite      func(byte)
}

func fakeIdentity() Identity {
	model := make([]byte, 24)
	model[0] = 0x3c
	return Identity{ModelCode: 0x3c, ModelReply: hex.EncodeToString(model), ParametersReply: strings.Repeat("00", 24)}
}
func newFake() *fakeUSB { return &fakeUSB{identity: fakeIdentity()} }
func (f *fakeUSB) Write(b []byte) (int, error) {
	f.writes = append(f.writes, append([]byte(nil), b...))
	if len(b) == 32 && bytes.Equal(b[:17], []byte("KinSealStudioKiss")) {
		f.last = b[17]
		if f.onWrite != nil {
			f.onWrite(f.last)
		}
	} else if f.shortPayload {
		return len(b) - 1, nil
	}
	return len(b), nil
}
func (f *fakeUSB) Read(b []byte) (int, error) {
	f.reads++
	if f.last == f.readError {
		return 0, errors.New("simulated timeout")
	}
	var response []byte
	switch f.last {
	case 0x31:
		response, _ = hex.DecodeString(f.identity.ModelReply)
	case 0x53:
		response, _ = hex.DecodeString(f.identity.ParametersReply)
	default:
		response = []byte{'O'}
		if f.ack != 0 {
			response[0] = f.ack
		}
	}
	if f.last == f.nack {
		response = []byte{'E'}
	}
	return copy(b, response), nil
}
func (f *fakeUSB) Close() error { return nil }
func (f *fakeUSB) commands() []byte {
	var c []byte
	for _, b := range f.writes {
		if len(b) == 32 && bytes.Equal(b[:17], []byte("KinSealStudioKiss")) {
			c = append(c, b[17])
		}
	}
	return c
}

func TestFullTransferAndCommitBoundary(t *testing.T) {
	f := newFake()
	s := Session{IO: f}
	if err := s.upload(fixturePackage(t), f.identity); err != nil {
		t.Fatal(err)
	}
	if s.status() != "commit_confirmed_reset_sent" {
		t.Fatal(s.status())
	}
	want := []byte{0x31, 0x53, 0x34, 0x35, 0x0a, 0x35, 0x0c, 0x35, 0x0e, 0x35, 0x16, 0x35, 0x15, 0x35, 0x19, 0x35, 0x12, 0x35, 0x14, 0x35, 0x0d, 0x35, 0x1e, 0x35, 0x0f, 0x35, 0x11, 0x35, 0x1b, 0x1a}
	if !bytes.Equal(f.commands(), want) {
		t.Fatalf("commands %x", f.commands())
	}
	if len(f.writes) != 40 {
		t.Fatalf("expected 30 commands + 10 payloads, got %d", len(f.writes))
	}
	if f.reads != 29 {
		t.Fatalf("unexpected replies %d", f.reads)
	}
}

func TestFailureStopsWithoutCommitOrReset(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		setup    func(*fakeUSB)
		expected Identity
		status   string
	}{
		{"identity-mismatch", func(f *fakeUSB) {
			f.identity.ModelCode = 0x22
			f.identity.ModelReply = "22" + f.identity.ModelReply[2:]
		}, fakeIdentity(), "no_download_confirmed"},
		{"start-nack", func(f *fakeUSB) { f.nack = 0x34 }, fakeIdentity(), "download_start_status_unknown"},
		{"data-nack", func(f *fakeUSB) { f.nack = 0x0c }, fakeIdentity(), "partial_transfer_no_commit_confirmation"},
		{"short-write", func(f *fakeUSB) { f.shortPayload = true }, fakeIdentity(), "partial_transfer_no_commit_confirmation"},
		{"commit-timeout", func(f *fakeUSB) { f.readError = 0x1b }, fakeIdentity(), "commit_status_unknown"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newFake()
			scenario.setup(f)
			s := Session{IO: f}
			if err := s.upload(fixturePackage(t), scenario.expected); err == nil {
				t.Fatal("failure accepted")
			}
			if s.status() != scenario.status {
				t.Fatal(s.status())
			}
			if bytes.Contains(f.commands(), []byte{0x1a}) {
				t.Fatal("reset after failure")
			}
			if scenario.name != "commit-timeout" && bytes.Contains(f.commands(), []byte{0x1b}) {
				t.Fatal("committed after failure")
			}
		})
	}
}

func TestCancelAndLogFailureBoundaries(t *testing.T) {
	f := newFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := Session{IO: f, Context: ctx}
	if err := s.upload(fixturePackage(t), f.identity); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(f.writes) != 0 {
		t.Fatal("write after early cancellation")
	}
	f = newFake()
	s = Session{IO: f, Log: func(Event) error { return errors.New("disk full") }}
	if err := s.upload(fixturePackage(t), f.identity); err == nil {
		t.Fatal("log failure ignored")
	}
	if len(f.writes) != 0 {
		t.Fatal("unlogged USB write")
	}
	f = newFake()
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	f.onWrite = func(cmd byte) {
		if cmd == 0x1b {
			cancel()
		}
	}
	s = Session{IO: f, Context: ctx}
	if err := s.upload(fixturePackage(t), f.identity); err != nil {
		t.Fatal(err)
	}
	if !s.ResetWritten {
		t.Fatal("cancellation after commit prevented reset completion")
	}
}

func TestTargetBinding(t *testing.T) {
	p := fixturePackage(t)
	id := fakeIdentity()
	if checkTarget(p, "AMZ043W01", id, false) == nil {
		t.Fatal("unverified AMZ accepted by default")
	}
	if checkTarget(p, "wrong-model", id, true) == nil {
		t.Fatal("target model mismatch ignored")
	}
	if err := checkTarget(p, "AMZ043W01", id, true); err != nil {
		t.Fatal(err)
	}
}

func TestUSBSelection(t *testing.T) {
	if !matchesDevice(0x7365, 0x616c, USBOptions{}) || !matchesDevice(0x5345, 0x1234, USBOptions{}) {
		t.Fatal("known USB IDs missing")
	}
	if matchesDevice(0x1234, 0x5678, USBOptions{}) {
		t.Fatal("unrelated device accepted")
	}
	if !matchesDevice(0x1234, 0x5678, USBOptions{VID: 0x1234, PID: 0x5678}) {
		t.Fatal("explicit VID/PID failed")
	}
	if got, err := parseUSBID("0x7365"); err != nil || got != 0x7365 {
		t.Fatal(got, err)
	}
	if _, err := parseUSBID("0x10000"); err == nil {
		t.Fatal("ID overflow")
	}
}
