package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const version = "0.2.7"

type ProbeReport struct {
	Format   string   `json:"format"`
	Created  string   `json:"created"`
	Device   Device   `json:"device"`
	Identity Identity `json:"identity"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Printf(`Kinseal Uploader %s (%s/%s)

  inspect --file load.kns
  list [--all | --vid 0x7365 --pid 0x616c]
  usb-info --out usb-info.json [--vid ... --pid ...] [--path ...]
  probe --out display.json [--vid ... --pid ...] [--path ...] [--guid ...]
  upload --file load.kns --probe display.json --model AMZ043W01 --experimental

inspect öffnet kein USB-Gerät. probe fragt nur Identität/Parameter ab.
upload ersetzt das HMI-Projekt und sendet einen Neustart. Für AMZ ist der
beobachtete USB-Protokollpfad experimentell und noch am Gerät zu verifizieren.
Kein Firmware-Flasher. Keine Treiberinstallation. Windows: WinUSB; macOS: libusb.
Kommandospezifische Optionen: <Kommando> --help
`, version, runtime.GOOS, runtime.GOARCH)
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage()
		return nil
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Printf("kinseal-uploader %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
		return nil
	}
	cmd := args[0]
	if cmd != "inspect" && cmd != "list" && cmd != "probe" && cmd != "upload" && cmd != "usb-info" {
		return fmt.Errorf("unbekanntes Kommando %q", cmd)
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	var file, out, probe, model, logPath, path, guidText, vidText, pidText string
	var all, experimental, detachKernelDriver bool
	var timeout uint
	if cmd == "inspect" || cmd == "upload" {
		fs.StringVar(&file, "file", "", "kompiliertes Studio-Archiv (.kns/ZIP, unabhängig von Endung)")
	}
	if cmd != "inspect" {
		fs.StringVar(&vidText, "vid", "0", "USB Vendor ID, z.B. 0x7365; 0 = bekannte Kinseal-Paare")
		fs.StringVar(&pidText, "pid", "0", "USB Product ID, z.B. 0x616c; 0 = automatisch")
		fs.StringVar(&guidText, "guid", "", "optionale registrierte DeviceInterfaceGUID")
	}
	if cmd == "list" {
		fs.BoolVar(&all, "all", false, "alle USB-VID/PID-Geräte anzeigen")
	}
	if cmd == "probe" || cmd == "upload" || cmd == "usb-info" {
		fs.StringVar(&path, "path", "", "exakter USB-Interfacepfad bei mehreren Geräten")
		fs.UintVar(&timeout, "timeout-ms", 10000, "Timeout pro USB-Transfer (100..120000 ms)")
	}
	if runtime.GOOS == "darwin" && (cmd == "probe" || cmd == "upload") {
		fs.BoolVar(&detachKernelDriver, "detach-kernel-driver", false, "macOS: libusb darf den Kernel-Treiber voruebergehend abloesen (ggf. sudo erforderlich)")
	}
	if cmd == "probe" || cmd == "usb-info" {
		fs.StringVar(&out, "out", "", "neue JSON-Datei für Gerätebericht (erforderlich)")
	}
	if cmd == "upload" {
		fs.StringVar(&probe, "probe", "", "Gerätebericht aus probe (erforderlich)")
		fs.StringVar(&model, "model", "", "exakter Modellname im Studio-Projekt (erforderlich)")
		fs.StringVar(&logPath, "log", "", "neue JSONL-Protokolldatei; Standard: kinseal-upload-<UTC>.jsonl")
		fs.BoolVar(&experimental, "experimental", false, "USB-Protokoll auf noch nicht verifiziertem Modell versuchen")
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unerwartete Argumente %q; Optionen vor Positionsargumenten angeben", fs.Args())
	}
	if cmd == "inspect" || cmd == "upload" {
		if file == "" {
			return fmt.Errorf("--file fehlt")
		}
	}
	var p *Package
	if file != "" {
		var err error
		p, err = loadPackage(file)
		if err != nil {
			return err
		}
	}
	if cmd == "inspect" {
		return printJSON(os.Stdout, p)
	}
	vid, err := parseUSBID(vidText)
	if err != nil {
		return err
	}
	pid, err := parseUSBID(pidText)
	if err != nil {
		return err
	}
	o := USBOptions{VID: vid, PID: pid, GUID: guidText, Path: path, All: all, TimeoutMS: uint32(timeout), DetachKernelDriver: detachKernelDriver}
	if cmd == "list" {
		devices, err := discoverUSB(o)
		if err != nil {
			return err
		}
		if devices == nil {
			devices = []Device{}
		}
		return printJSON(os.Stdout, devices)
	}
	if timeout < 100 || timeout > 120000 {
		return fmt.Errorf("--timeout-ms muss 100..120000 sein")
	}
	if cmd == "usb-info" {
		if out == "" {
			return fmt.Errorf("--out fehlt, z.B. --out usb-info.json")
		}
		f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		keep := false
		defer func() {
			f.Close()
			if !keep {
				os.Remove(out)
			}
		}()
		report, err := inspectUSB(o)
		if err != nil {
			return err
		}
		if err := printJSON(f, report); err != nil {
			return err
		}
		if err := f.Sync(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		keep = true
		return printJSON(os.Stdout, report)
	}
	if cmd == "probe" {
		if out == "" {
			return fmt.Errorf("--out fehlt, z.B. --out display.json")
		}
		// Reserve without replacing anything, before USB. No report is published
		// on failure, and no identity report can overwrite an input project.
		f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		keep := false
		defer func() {
			f.Close()
			if !keep {
				os.Remove(out)
			}
		}()
		t, device, err := openUSB(o)
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		s := Session{IO: t, Context: ctx}
		id, probeErr := s.identify()
		closeErr := t.Close()
		if probeErr != nil {
			return probeErr
		}
		if closeErr != nil {
			return closeErr
		}
		report := ProbeReport{Format: "kinseal-usb-probe-v1", Created: time.Now().UTC().Format(time.RFC3339), Device: device, Identity: id}
		if err := printJSON(f, report); err != nil {
			return err
		}
		if err := f.Sync(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		keep = true
		return printJSON(os.Stdout, report)
	}
	if probe == "" || model == "" {
		return fmt.Errorf("upload benötigt --probe und --model")
	}
	report, err := loadProbe(probe)
	if err != nil {
		return err
	}
	if err := checkTarget(p, model, report.Identity, experimental); err != nil {
		return err
	}
	// Bind upload to the same reported instance/interface, not just a VID/PID.
	if o.VID != 0 && o.VID != report.Device.VID || o.PID != 0 && o.PID != report.Device.PID {
		return fmt.Errorf("VID/PID weicht vom Probebericht ab")
	}
	o.VID, o.PID = report.Device.VID, report.Device.PID
	if o.Path == "" {
		o.Path = report.Device.Paths[0]
	}
	if logPath == "" {
		logPath = "kinseal-upload-" + time.Now().UTC().Format("20060102T150405.000000000") + ".jsonl"
	}
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("Logdatei: %w", err)
	}
	defer logFile.Close()
	encoder := json.NewEncoder(logFile)
	logObject := func(v any) error {
		if err := encoder.Encode(v); err != nil {
			return err
		}
		return logFile.Sync()
	}
	if err := logObject(map[string]any{"format": "kinseal-upload-log-v1", "version": version, "package": p, "probe": report, "experimental": experimental}); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Projekt %s, SHA256 %s\nLog: %s\n", p.SourceModel, p.SHA256, logPath)
	t, device, err := openUSB(o)
	if err != nil {
		return err
	}
	if !strings.EqualFold(device.Instance, report.Device.Instance) {
		t.Close()
		return fmt.Errorf("Geräteinstanz hat sich geändert; neuen Probebericht erstellen")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	s := Session{IO: t, Context: ctx, Log: func(e Event) error {
		if e.Operation == "payload-write" {
			fmt.Fprintf(os.Stderr, "  %-22s %d/%d Bytes\n", e.Segment, e.Returned, e.Requested)
		}
		return logObject(e)
	}}
	uploadErr := s.upload(p, report.Identity)
	closeErr := t.Close()
	result := map[string]any{"status": s.status(), "start_attempted": s.StartAttempted, "start_acknowledged": s.Started, "commit_attempted": s.CommitAttempted, "commit_acknowledged": s.Committed, "reset_written": s.ResetWritten, "screen_verified": false}
	if uploadErr != nil {
		result["error"] = uploadErr.Error()
	}
	if closeErr != nil {
		result["close_error"] = closeErr.Error()
	}
	logErr := logObject(result)
	if err := printJSON(os.Stdout, result); err != nil {
		return err
	}
	if uploadErr != nil {
		return fmt.Errorf("%s: %w; nicht automatisch wiederholen", s.status(), uploadErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if logErr != nil {
		return logErr
	}
	fmt.Fprintln(os.Stderr, "USB-Abschlussantwort empfangen, Neustart gesendet. Projektübernahme unbestätigt; Anzeige und Touch am Display prüfen.")
	return nil
}

func printJSON(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func parseUSBID(s string) (uint16, error) {
	base := 10
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		base = 16
		s = s[2:]
	}
	v, err := strconv.ParseUint(s, base, 16)
	if err != nil {
		return 0, fmt.Errorf("ungültige USB-ID %q: %w (hex mit 0x angeben)", s, err)
	}
	return uint16(v), nil
}

func loadProbe(path string) (ProbeReport, error) {
	var p ProbeReport
	f, err := os.Open(path)
	if err != nil {
		return p, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return p, err
	}
	if st.Size() > 65536 {
		return p, fmt.Errorf("Probebericht größer als 64 KiB")
	}
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return p, fmt.Errorf("Probebericht enthält zusätzliche Daten")
	}
	if p.Format != "kinseal-usb-probe-v1" || p.Device.VID == 0 || p.Device.PID == 0 || p.Device.Instance == "" || len(p.Device.Paths) != 1 || p.Device.Paths[0] == "" {
		return p, fmt.Errorf("unvollständiger Probebericht")
	}
	m, e1 := hex.DecodeString(p.Identity.ModelReply)
	a, e2 := hex.DecodeString(p.Identity.ParametersReply)
	if e1 != nil || e2 != nil || len(m) != 24 || len(a) != 24 || m[0] != p.Identity.ModelCode || a[0] != 0 {
		return p, fmt.Errorf("ungültige Geräteantwort im Probebericht")
	}
	return p, nil
}
