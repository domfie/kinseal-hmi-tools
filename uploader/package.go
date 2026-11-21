package main

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
)

const maxPackageBytes = 256 << 20
const amzProfile = "amz043w01-studio-5.1-static"

type Segment struct {
	Name       string `json:"name"`
	Command    byte   `json:"command"`
	Length     int    `json:"length"`
	SHA256     string `json:"sha256"`
	Options    []byte `json:"options,omitempty"`
	OptionsHex string `json:"options_hex,omitempty"`
	Data       []byte `json:"-"`
}

type Package struct {
	File          string       `json:"file"`
	SHA256        string       `json:"sha256"`
	SourceModel   string       `json:"source_model"`
	SourceFormat  string       `json:"source_format"`
	Segments      []Segment    `json:"segments"`
	Ignored       []string     `json:"excluded_or_empty_files"`
	ArchiveMember string       `json:"archive_member,omitempty"`
	ArchiveSHA256 string       `json:"archive_sha256"`
	Profile       string       `json:"protocol_profile"`
	PageCount     int          `json:"page_count,omitempty"`
	Files         []FileRecord `json:"files"`
	Excluded      []FileRecord `json:"excluded_from_project_upload,omitempty"`
}

type FileRecord struct {
	Name   string `json:"name"`
	Length int    `json:"length"`
	SHA256 string `json:"sha256"`
	Role   string `json:"role"`
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Only the observed compiled archive layout is accepted. A .sm or arbitrary .bin
// is not a download image. No extraction to disk, filename-based execution, or
// substitution with another display's fixture takes place.
func loadPackage(path string) (*Package, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() <= 0 || st.Size() > maxPackageBytes {
		return nil, fmt.Errorf("Dateigröße außerhalb 1..%d Bytes", maxPackageBytes)
	}
	// Parse and hash one immutable snapshot. Studio saving the file concurrently
	// must not mix old payload bytes with a hash of a newer file version.
	raw, err := io.ReadAll(io.LimitReader(f, maxPackageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) != int(st.Size()) {
		return nil, fmt.Errorf("Exportdatei während des Lesens geändert; erneut exportieren")
	}
	archive, member, err := unwrapArchive(raw)
	if err != nil {
		return nil, err
	}
	r, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("kein unterstütztes kompiliertes ZIP/KNS-Archiv (%w); eine rohe .bin oder .sm kann nicht ungeprüft gesendet werden", err)
	}
	if len(r.File) > 64 {
		return nil, fmt.Errorf("zu viele Archiveinträge: %d", len(r.File))
	}
	files := map[string][]byte{}
	var total int64
	for _, z := range r.File {
		name := z.Name
		if z.FileInfo().IsDir() || filepath.Base(name) != name || strings.ContainsAny(name, "/\\:") || name == "." || name == ".." {
			return nil, fmt.Errorf("unerwarteter Archivpfad %q", name)
		}
		if z.Mode()&os.ModeType != 0 {
			return nil, fmt.Errorf("kein regulärer Archiveintrag: %q", name)
		}
		key := strings.ToLower(name)
		if _, exists := files[key]; exists {
			return nil, fmt.Errorf("doppelter Archiveintrag %q", name)
		}
		if z.UncompressedSize64 > maxPackageBytes || uint64(total)+z.UncompressedSize64 > maxPackageBytes {
			return nil, fmt.Errorf("Archiv entpackt größer als 256 MiB")
		}
		entry, err := z.Open()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		b, readErr := io.ReadAll(io.LimitReader(entry, int64(maxPackageBytes)-total+1))
		closeErr := entry.Close()
		if readErr != nil {
			return nil, fmt.Errorf("%s (inklusive ZIP-CRC): %w", name, readErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		total += int64(len(b))
		if total > maxPackageBytes || uint64(len(b)) != z.UncompressedSize64 {
			return nil, fmt.Errorf("ungültige entpackte Länge: %s", name)
		}
		files[key] = b
	}
	p, err := preparePackage(files)
	if err != nil {
		return nil, err
	}
	p.File = path
	p.SHA256 = digest(raw)
	p.ArchiveSHA256 = digest(archive)
	p.ArchiveMember = member
	return p, nil
}

func preparePackage(files map[string][]byte) (*Package, error) {
	format, model, err := snapshotIdentity(files["upload.knr"])
	if err != nil {
		return nil, fmt.Errorf("upLoad.knr: %w", err)
	}
	amz := model == "AMZ043W01"
	p := &Package{SourceFormat: format, SourceModel: model, Profile: "amt-observed-bulk-v1"}
	if amz {
		p.Profile = amzProfile
		if len(files["sheetpara.dat"]) == 0 || len(files["sheetpara.dat"])%72 != 0 {
			return nil, fmt.Errorf("SheetPara.dat hat kein bekanntes 72-Byte-Seitenraster")
		}
		p.PageCount = len(files["sheetpara.dat"]) / 72
		if p.PageCount > 200 {
			return nil, fmt.Errorf("mehr als 200 Seiten")
		}
	}
	excluded := map[string]string{"load.bin": "media_descriptor", "usbload.bin": "media_loader_metadata"}
	if amz {
		excluded[strings.ToLower(model)+".bin"] = "optional_firmware_update_not_sent"
		excluded["hz.bin"] = "optional_font_update_not_sent"
	}
	var programs []string
	for name, data := range files {
		if strings.HasSuffix(name, ".bin") && excluded[name] == "" && len(data) != 0 {
			programs = append(programs, name)
		}
	}
	if len(programs) != 1 {
		return nil, fmt.Errorf("genau eine Protokoll-.bin erwartet, gefunden %d; mehrere Treiber/Ports werden noch nicht unterstützt", len(programs))
	}
	program := programs[0]
	if !strings.HasSuffix(program, "_com1.bin") {
		return nil, fmt.Errorf("kein belegter COM1-Protokolltreiber: %s", program)
	}
	if descriptor := files["load.bin"]; len(descriptor) > 0 {
		if err := validateDescriptor(descriptor, format, model, program, p.PageCount); err != nil {
			return nil, err
		}
	}
	specs := []struct {
		name    string
		command byte
		mode    byte // 0: required; 1: omit if empty; 2: command even if absent/empty
	}{
		{"syspara.dat", 0x0a, 0}, {"controlpara.dat", 0x0b, 1}, {"logopixel.dat", 0x1f, 1},
		{"sheetpara.dat", 0x0c, 0}, {"marcovar.dat", 0x36, 1}, {"marcorun.dat", 0x1d, 1},
		{"alarmpara.dat", 0x0e, 0}, {"alarmmuti.dat", 0x16, 2}, {"collect.dat", 0x15, 2},
		{"recipe.dat", 0x19, 2}, {"addrpara.dat", 0x12, 0}, {"piclenpara.dat", 0x14, 0},
		{"curve.dat", 0x0d, 0}, {"upload.knr", 0x1e, 0}, {"controlpixel.dat", 0x10, 1},
		{"sheetpixel.dat", 0x0f, 0}, {program, 0x11, 0},
	}
	for _, pair := range [][2]string{{"controlpara.dat", "controlpixel.dat"}, {"marcovar.dat", "marcorun.dat"}} {
		if (len(files[pair[0]]) > 0) != (len(files[pair[1]]) > 0) {
			return nil, fmt.Errorf("unvollständiges Datenpaar %s / %s", pair[0], pair[1])
		}
	}
	known := map[string]bool{}
	for name := range excluded {
		known[name] = true
	}
	for _, spec := range specs {
		known[spec.name] = true
		data := files[spec.name]
		if !amz && len(data) > 0 && (spec.command == 0x16 || spec.command == 0x19 || spec.command == 0x1f) {
			return nil, fmt.Errorf("%s ist außerhalb des statisch untersuchten AMZ043W01-Profils noch nicht freigegeben", spec.name)
		}
		if len(data) == 0 {
			if spec.mode == 1 {
				continue
			}
			if spec.mode == 0 {
				return nil, fmt.Errorf("Pflichtsegment fehlt oder ist leer: %s", spec.name)
			}
		}
		segment := Segment{Name: spec.name, Command: spec.command, Data: data}
		if amz && spec.command == 0x0b {
			// The successful Studio capture (frame 79) uses block index 00 00,
			// not the page-count variant seen in another static code path.
			// Leave options unset so frameSegments emits the block index.
			if len(data) > 0x40000 {
				return nil, fmt.Errorf("ControlPara.dat benötigt einen größeren, noch nicht mit diesem Studio abgeglichenen Blockpfad")
			}
		}
		if spec.command == 0x1e {
			framed := make([]byte, 4+len(data))
			binary.BigEndian.PutUint32(framed, uint32(len(data)))
			copy(framed[4:], data)
			segment.Data = framed
		}
		if spec.command == 0x11 {
			segment.Options = make([]byte, 4)
			binary.LittleEndian.PutUint32(segment.Options, uint32(len(data)))
		}
		parts, err := frameSegments(segment, amz)
		if err != nil {
			return nil, err
		}
		p.Segments = append(p.Segments, parts...)
	}
	for name, b := range files {
		if !known[name] {
			return nil, fmt.Errorf("unbekannter Archiveintrag %q: keine Daten werden stillschweigend ausgelassen", name)
		}
		role := "project_payload"
		if reason := excluded[name]; reason != "" {
			role = reason
		}
		record := FileRecord{Name: name, Length: len(b), SHA256: digest(b), Role: role}
		p.Files = append(p.Files, record)
		if excluded[name] != "" {
			p.Excluded = append(p.Excluded, record)
		}
		if excluded[name] != "" || len(b) == 0 {
			p.Ignored = append(p.Ignored, name)
		}
	}
	sort.Strings(p.Ignored)
	sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Name < p.Files[j].Name })
	sort.Slice(p.Excluded, func(i, j int) bool { return p.Excluded[i].Name < p.Excluded[j].Name })
	return p, nil
}

func snapshotIdentity(b []byte) (string, string, error) {
	if len(b) < 6 {
		return "", "", fmt.Errorf("kein qCompress-Schnappschuss")
	}
	expected := binary.BigEndian.Uint32(b)
	if expected == 0 || expected > maxPackageBytes {
		return "", "", fmt.Errorf("ungültige entpackte Länge %d", expected)
	}
	r, err := zlib.NewReader(bytes.NewReader(b[4:]))
	if err != nil {
		return "", "", err
	}
	defer r.Close()
	source, err := io.ReadAll(io.LimitReader(r, int64(expected)+1))
	if err != nil || len(source) != int(expected) {
		return "", "", fmt.Errorf("beschädigte zlib-Daten oder Längenabweichung: %v", err)
	}
	pos := 0
	format, err := readQString(source, &pos)
	if err != nil || !strings.HasPrefix(format, "KinSealStudio-") {
		return "", "", fmt.Errorf("unbekanntes natives Quellformat")
	}
	if len(source)-pos < 4 {
		return "", "", fmt.Errorf("fehlendes Feld vor Modellname")
	}
	pos += 4 // Observed intermediate version/block field; its meaning is not assumed.
	model, err := readQString(source, &pos)
	if err != nil || len(model) < 6 || len(model) > 100 {
		return "", "", fmt.Errorf("Modellname nicht lesbar")
	}
	for _, c := range model {
		if c < 32 || c > 126 || strings.ContainsRune("/\\:", c) {
			return "", "", fmt.Errorf("ungültiger Modellname")
		}
	}
	return format, model, nil
}

func readQString(b []byte, pos *int) (string, error) {
	if *pos < 0 || len(b)-*pos < 4 {
		return "", io.ErrUnexpectedEOF
	}
	n := binary.BigEndian.Uint32(b[*pos:])
	*pos += 4
	if n > 4096 || n%2 != 0 || uint64(n) > uint64(len(b)-*pos) {
		return "", fmt.Errorf("ungültige QString-Länge %d", n)
	}
	units := make([]uint16, int(n)/2)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(b[*pos+2*i:])
	}
	*pos += int(n)
	return string(utf16.Decode(units)), nil
}
