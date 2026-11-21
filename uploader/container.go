package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// Accept one transport wrapper such as kinseal.zip/kinseal/compile/load.kns.
// The inner archive is authoritative. Sibling DAT/BIN copies must be identical;
// mismatched compilations are rejected rather than mixed into a new package.
func unwrapArchive(raw []byte) ([]byte, string, error) {
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, "", fmt.Errorf("kein kompiliertes ZIP/KNS-Archiv: %w", err)
	}
	var selected string
	for _, f := range r.File {
		if !f.FileInfo().IsDir() && strings.EqualFold(path.Base(f.Name), "load.kns") {
			if selected != "" {
				return nil, "", fmt.Errorf("mehrere load.kns im ZIP: keine eindeutige Auswahl")
			}
			selected = f.Name
		}
	}
	if selected == "" {
		return raw, "", nil
	}
	outer, err := readWrapperEntries(r)
	if err != nil {
		return nil, "", err
	}
	inner := outer[strings.ToLower(selected)]
	ir, err := zip.NewReader(bytes.NewReader(inner), int64(len(inner)))
	if err != nil {
		return nil, "", fmt.Errorf("eingebettete load.kns: %w", err)
	}
	entries, err := readWrapperEntries(ir)
	if err != nil {
		return nil, "", err
	}
	prefix := strings.ToLower(path.Dir(selected)) + "/"
	if prefix == "./" {
		prefix = ""
	}
	for name, b := range outer {
		if name == strings.ToLower(selected) {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			return nil, "", fmt.Errorf("unerwartete Datei neben compile-Verzeichnis: %s", name)
		}
		base := strings.TrimPrefix(name, prefix)
		original, ok := entries[base]
		if !ok || !bytes.Equal(b, original) {
			return nil, "", fmt.Errorf("äußere Datei %s stimmt nicht mit load.kns überein", name)
		}
	}
	return inner, selected, nil
}

func readWrapperEntries(r *zip.Reader) (map[string][]byte, error) {
	if len(r.File) > 64 {
		return nil, fmt.Errorf("mehr als 64 Archiveinträge")
	}
	files := map[string][]byte{}
	seen := map[string]bool{}
	total := int64(0)
	for _, f := range r.File {
		name := strings.TrimSuffix(f.Name, "/")
		if name == "" || name == "." || path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\:") || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("ungültiger Archivpfad %q", f.Name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("doppelter Archivpfad %q", name)
		}
		seen[key] = true
		if f.FileInfo().IsDir() {
			if f.UncompressedSize64 != 0 {
				return nil, fmt.Errorf("Verzeichnis mit Nutzdaten: %q", name)
			}
			continue
		}
		if f.Mode()&os.ModeType != 0 {
			return nil, fmt.Errorf("kein regulärer Archiveintrag: %q", name)
		}
		if f.UncompressedSize64 > maxPackageBytes || uint64(total)+f.UncompressedSize64 > maxPackageBytes {
			return nil, fmt.Errorf("entpacktes Archiv größer als 256 MiB")
		}
		e, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, readErr := io.ReadAll(io.LimitReader(e, maxPackageBytes-total+1))
		closeErr := e.Close()
		if readErr != nil {
			return nil, fmt.Errorf("%s: %w", name, readErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		total += int64(len(b))
		if total > maxPackageBytes || uint64(len(b)) != f.UncompressedSize64 {
			return nil, fmt.Errorf("falsche Länge für %s", name)
		}
		files[key] = b
	}
	return files, nil
}

func validateDescriptor(b []byte, format, model, program string, pages int) error {
	pos := 0
	f, err := readQString(b, &pos)
	if err != nil || f != format || len(b)-pos < 4 {
		return fmt.Errorf("load.bin hat keinen zum Projekt passenden Header")
	}
	pos += 4
	m, err := readQString(b, &pos)
	if err != nil || m != model {
		return fmt.Errorf("load.bin und upLoad.knr nennen unterschiedliche Modelle")
	}
	if pages > 0 && len(b)-pos >= 4 && binary.BigEndian.Uint32(b[pos:]) != uint32(pages) {
		return fmt.Errorf("Seitenzahl in load.bin stimmt nicht mit SheetPara.dat überein")
	}
	// Only interpret bounded, length-prefixed Link/... declarations, not arbitrary
	// raw string matches. No referenced path is opened or executed on the host.
	declared := map[string]bool{}
	for at := pos; at+4 <= len(b); at++ {
		end := at
		s, e := readQString(b, &end)
		if e != nil {
			continue
		}
		s = strings.ToLower(strings.ReplaceAll(s, "\\", "/"))
		if strings.HasPrefix(s, "link/") && strings.HasSuffix(s, ".bin") {
			declared[path.Base(s)] = true
		}
	}
	if len(declared) > 1 {
		return fmt.Errorf("load.bin referenziert mehrere PLC-Treiber; noch nicht unterstützt")
	}
	if len(declared) == 1 && !declared[program] {
		return fmt.Errorf("PLC-Treiber %s passt nicht zur Deklaration in load.bin", program)
	}
	return nil
}

// Chunk layouts come from static analysis of the manufacturer's LoadTool 5.1,
// not from assumptions about libusb/WinUSB maximum request sizes. See analysis/.
func frameSegments(s Segment, amz bool) ([]Segment, error) {
	const chunkSize = 0x40000
	chunked := amz && (s.Command == 0x0b || s.Command == 0x1e || s.Command == 0x10 || s.Command == 0x0f || s.Command == 0x1f)
	if amz && s.Command == 0x0b && len(s.Options) > 0 {
		if len(s.Data) > chunkSize {
			return nil, fmt.Errorf("ControlPara: kurzer Studio-Blockpfad überschritten")
		}
		chunked = false // Preserve Studio's page-count parameter, not a chunk number.
	}
	if !chunked || len(s.Data) == 0 {
		s.Length = len(s.Data)
		if len(s.Data) > 0 {
			s.SHA256 = digest(s.Data)
		}
		s.OptionsHex = hex.EncodeToString(s.Options)
		return []Segment{s}, nil
	}
	count := (len(s.Data) + chunkSize - 1) / chunkSize
	if count > 100 {
		return nil, fmt.Errorf("%s: mehr als 100 beobachtete Chunknummern nicht unterstützt", s.Name)
	}
	var result []Segment
	for offset, index := 0, 0; offset < len(s.Data); offset, index = offset+chunkSize, index+1 {
		end := offset + chunkSize
		if end > len(s.Data) {
			end = len(s.Data)
		}
		part := s
		part.Data = s.Data[offset:end]
		if count > 1 {
			part.Name = fmt.Sprintf("%s#%02d", s.Name, index)
		}
		number := []byte{byte(index / 10), byte(index % 10)}
		switch s.Command {
		case 0x0b, 0x1e:
			part.Options = number
		case 0x10, 0x0f:
			part.Options = make([]byte, 6)
			copy(part.Options, number)
			binary.BigEndian.PutUint32(part.Options[2:], uint32(len(part.Data)))
		case 0x1f:
			part.Options = make([]byte, 6)
			binary.LittleEndian.PutUint32(part.Options, uint32(len(s.Data)))
			copy(part.Options[4:], number)
		}
		part.Length = len(part.Data)
		part.SHA256 = digest(part.Data)
		part.OptionsHex = hex.EncodeToString(part.Options)
		result = append(result, part)
	}
	return result, nil
}
