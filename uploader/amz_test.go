package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAMZExportExtensions(t *testing.T) {
	m := fixtureFiles()
	delete(m, "collect.dat") // The supplied Studio export has no collection data.
	for name, data := range map[string][]byte{
		"controlpara.dat": {1}, "controlpixel.dat": {2}, "marcovar.dat": {3}, "marcorun.dat": {4},
		"recipe.dat": {5}, "logopixel.dat": {6, 7, 8}, "amz043w01.bin": []byte("FIRMWARE-DO-NOT-SEND"), "hz.bin": []byte("FONT-DO-NOT-SEND"),
	} {
		m[name] = data
	}
	p, err := preparePackage(m)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x0a, 0x0b, 0x1f, 0x0c, 0x36, 0x1d, 0x0e, 0x16, 0x15, 0x19, 0x12, 0x14, 0x0d, 0x1e, 0x10, 0x0f, 0x11}
	var got []byte
	for _, s := range p.Segments {
		got = append(got, s.Command)
		if bytes.Contains(s.Data, []byte("DO-NOT-SEND")) {
			t.Fatal("firmware/font entered upload plan")
		}
		if s.Command == 0x1f && !bytes.Equal(s.Options, []byte{3, 0, 0, 0, 0, 0}) {
			t.Fatalf("logo framing %x", s.Options)
		}
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("commands %x", got)
	}
	if len(p.Excluded) != 4 {
		t.Fatalf("expected firmware, font and two media metadata files: %+v", p.Excluded)
	}
	f := newFake()
	s := Session{IO: f}
	if err := s.upload(p, f.identity); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 55 {
		t.Fatalf("expected 40 command writes and 15 payloads, got %d", len(f.writes))
	}
	if s.status() != "commit_confirmed_reset_sent" {
		t.Fatal(s.status())
	}
	bad := f.identity
	bad.ModelCode = 0x4c
	if checkTarget(p, "AMZ043W01", bad, true) == nil {
		t.Fatal("wrong known model code bypassed experimental gate")
	}
}

func TestAMZChunkParametersAndConcatenation(t *testing.T) {
	data := bytes.Repeat([]byte{0xab}, 11*0x40000+37)
	for _, cmd := range []byte{0x0b, 0x1e, 0x10, 0x0f, 0x1f} {
		parts, err := frameSegments(Segment{Name: "data", Command: cmd, Data: data}, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) != 12 {
			t.Fatal(len(parts))
		}
		var joined []byte
		for i, p := range parts {
			joined = append(joined, p.Data...)
			pos := 0
			if cmd == 0x1f {
				pos = 4
			}
			if p.Options[pos] != byte(i/10) || p.Options[pos+1] != byte(i%10) {
				t.Fatalf("cmd %x, decimal chunk %d: %x", cmd, i, p.Options)
			}
			if (cmd == 0x10 || cmd == 0x0f) && int(binary.BigEndian.Uint32(p.Options[2:])) != len(p.Data) {
				t.Fatal("pixel chunk length must be BE32")
			}
			if cmd == 0x1f && int(binary.LittleEndian.Uint32(p.Options)) != len(data) {
				t.Fatal("logo total length must be LE32")
			}
			if p.Length != len(p.Data) || p.SHA256 != digest(p.Data) {
				t.Fatal("chunk metadata is not wire data")
			}
		}
		if !bytes.Equal(joined, data) {
			t.Fatal("chunk split changed payload")
		}
	}
}

func TestACKKRestrictedToStaticAMZProfile(t *testing.T) {
	p := fixturePackage(t)
	f := newFake()
	f.ack = 'K'
	s := Session{IO: f}
	if err := s.upload(p, f.identity); err != nil {
		t.Fatal(err)
	}
	p.Profile = "amt-observed-bulk-v1"
	f = newFake()
	f.ack = 'K'
	s = Session{IO: f}
	if err := s.upload(p, f.identity); err == nil {
		t.Fatal("unobserved generic ACK K accepted")
	}
}

func TestDescriptorConsistency(t *testing.T) {
	m := fixtureFiles()
	descriptor := append([]byte(nil), m["load.bin"]...)
	count := make([]byte, 4)
	binary.BigEndian.PutUint32(count, 2)
	m["load.bin"] = append(descriptor, count...)
	if _, err := preparePackage(m); err == nil {
		t.Fatal("inconsistent descriptor page count accepted")
	}
	binary.BigEndian.PutUint32(count, 1)
	var declaration bytes.Buffer
	name := "Link/serial_amz/MODBUS/other_com1.bin"
	binary.Write(&declaration, binary.BigEndian, uint32(2*len(name)))
	for _, c := range name {
		binary.Write(&declaration, binary.BigEndian, uint16(c))
	}
	m["load.bin"] = append(append(descriptor, count...), declaration.Bytes()...)
	if _, err := preparePackage(m); err == nil {
		t.Fatal("different declared PLC driver accepted")
	}
}

func wrapper(t *testing.T, inner []byte, extra map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, name := range []string{"kinseal/", "kinseal/compile/"} {
		if _, err := w.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	f, err := w.Create("kinseal/compile/load.kns")
	if err != nil {
		t.Fatal(err)
	}
	f.Write(inner)
	for name, data := range extra {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(data)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestNestedExportIsOneConsistentSnapshot(t *testing.T) {
	inner := zipFixture(t, nil)
	for _, tc := range []struct {
		name  string
		extra map[string][]byte
		valid bool
	}{
		{"only-inner", nil, true},
		{"matching-copy", map[string][]byte{"kinseal/compile/syspara.dat": fixtureFiles()["syspara.dat"]}, true},
		{"stale-copy", map[string][]byte{"kinseal/compile/syspara.dat": {99}}, false},
		{"traversal", map[string][]byte{"kinseal/compile/../syspara.dat": {1}}, false},
		{"second-archive", map[string][]byte{"other/load.kns": inner}, false},
		{"unknown-sibling", map[string][]byte{"kinseal/compile/surprise.bin": {1}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := wrapper(t, inner, tc.extra)
			path := filepath.Join(t.TempDir(), "kinseal.zip")
			os.WriteFile(path, data, 0600)
			p, err := loadPackage(path)
			if !tc.valid {
				if err == nil {
					t.Fatal("inconsistent wrapper accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.ArchiveMember != "kinseal/compile/load.kns" || p.ArchiveSHA256 != digest(inner) || p.SHA256 != digest(data) {
				t.Fatal("wrapper/archive provenance lost")
			}
		})
	}
}

// Optional local integration test. The proprietary user export is not checked
// into source or bundled with the executable. Golden wire facts are independent
// of preparePackage and were checked against the manufacturer's download code.
func TestSuppliedStudioExport(t *testing.T) {
	path := os.Getenv("KINSEAL_TEST_EXPORT")
	if path == "" {
		t.Skip("set KINSEAL_TEST_EXPORT to the supplied kinseal.zip")
	}
	p, err := loadPackage(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.ArchiveSHA256 != "899dd88b49c755a3eeb82090764f3f941bca1d96abcc1abb966a5b7e4f5c4006" {
		t.Fatal("unexpected integration export")
	}
	if p.SourceModel != "AMZ043W01" || len(p.Segments) != 17 || p.PageCount != 8 {
		t.Fatal(p.SourceModel, len(p.Segments))
	}
	want := map[byte]struct {
		length  int
		options string
	}{
		0x0a: {4998, ""}, 0x0b: {56320, "0000"}, 0x1f: {10809, "392a00000000"}, 0x0c: {576, ""},
		0x36: {1808, ""}, 0x1d: {4560, ""}, 0x0e: {47840, ""}, 0x16: {0, ""}, 0x15: {0, ""},
		0x19: {212104, ""}, 0x12: {200, ""}, 0x14: {4296, ""}, 0x0d: {501, ""}, 0x1e: {85356, "0000"},
		0x10: {133152, "000000020820"}, 0x0f: {66030, "0000000101ee"}, 0x11: {4628, "14120000"},
	}
	for _, s := range p.Segments {
		e, ok := want[s.Command]
		if !ok || e.length != s.Length || e.options != s.OptionsHex {
			t.Fatalf("unexpected segment %+v", s)
		}
		if s.Command == 0x1e && binary.BigEndian.Uint32(s.Data) != 85352 {
			t.Fatal("KNR prefix")
		}
		if strings.Contains(s.Name, "amz043w01") || s.Name == "hz.bin" {
			t.Fatal("firmware/font transmitted")
		}
	}
	f := newFake()
	s := Session{IO: f}
	if err := s.upload(p, f.identity); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 55 || !s.ResetWritten {
		t.Fatal("full export protocol simulation failed")
	}
}
