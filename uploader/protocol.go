package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

type Transport interface {
	Write([]byte) (int, error)
	Read([]byte) (int, error)
	Close() error
}

type Event struct {
	Time      string `json:"time"`
	Stage     string `json:"stage"`
	Operation string `json:"operation"`
	Command   byte   `json:"command,omitempty"`
	Segment   string `json:"segment,omitempty"`
	Requested int    `json:"requested,omitempty"`
	Returned  int    `json:"returned"`
	Hex       string `json:"hex,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Session struct {
	IO              Transport
	Context         context.Context
	Log             func(Event) error
	StartAttempted  bool
	Started         bool
	CommitAttempted bool
	Committed       bool
	ResetWritten    bool
	AcceptK         bool
}

func (s *Session) emit(e Event) error {
	e.Time = time.Now().UTC().Format(time.RFC3339Nano)
	if s.Log != nil {
		return s.Log(e)
	}
	return nil
}

func frame(command byte, options []byte) []byte {
	b := make([]byte, 32)
	copy(b, "KinSealStudioKiss")
	b[17] = command
	copy(b[18:], options)
	return b
}

func (s *Session) write(stage string, command byte, segment string, b []byte, payload bool) error {
	if s.Context != nil && !s.CommitAttempted {
		if err := s.Context.Err(); err != nil {
			return err
		}
	}
	e := Event{Stage: stage, Operation: "command-write", Command: command, Segment: segment, Requested: len(b)}
	if payload {
		e.Operation = "payload-write"
		e.SHA256 = digest(b)
	} else {
		e.Hex = hex.EncodeToString(b)
	}
	// Log intent before mutations: a log failure must not cause an unrecorded erase.
	intent := e
	intent.Operation += "-intent"
	if err := s.emit(intent); err != nil {
		return fmt.Errorf("Log vor USB-Schreiben: %w", err)
	}
	if command == 0x1b && !payload {
		s.CommitAttempted = true
	}
	if command == 0x34 && !payload {
		s.StartAttempted = true
	}
	n, err := s.IO.Write(b)
	e.Returned = n
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	if err != nil {
		e.Error = err.Error()
	}
	logErr := s.emit(e)
	if err != nil {
		return fmt.Errorf("%s/%s Schreiben %d/%d: %w", stage, segment, n, len(b), err)
	}
	if logErr != nil {
		return fmt.Errorf("USB geschrieben, Log fehlgeschlagen: %w", logErr)
	}
	return nil
}

func (s *Session) command(stage string, command byte, options []byte, expected int, first *byte) ([]byte, error) {
	if len(options) > 14 {
		return nil, fmt.Errorf("Befehlsparameter zu lang")
	}
	if err := s.write(stage, command, "", frame(command, options), false); err != nil {
		return nil, err
	}
	if expected == 0 {
		return nil, nil
	}
	capacity := 4
	if expected == 24 {
		capacity = 32
	}
	b := make([]byte, capacity)
	n, err := s.IO.Read(b)
	if n < 0 || n > len(b) {
		return nil, fmt.Errorf("ungültige Transport-Länge %d", n)
	}
	b = b[:n]
	if err == nil && n != expected {
		err = fmt.Errorf("Antwort %x: %d Bytes empfangen, %d erwartet", b, n, expected)
	}
	if err == nil && first != nil && (n == 0 || (b[0] != *first && !(s.AcceptK && *first == 'O' && b[0] == 'K'))) {
		err = fmt.Errorf("Antwort %x: erstes Byte 0x%02x erwartet", b, *first)
	}
	e := Event{Stage: stage, Operation: "reply-read", Command: command, Requested: capacity, Returned: n, Hex: hex.EncodeToString(b)}
	if err != nil {
		e.Error = err.Error()
	}
	logErr := s.emit(e)
	if err != nil {
		return b, fmt.Errorf("%s: %w", stage, err)
	}
	if logErr != nil {
		return b, logErr
	}
	return b, nil
}

type Identity struct {
	ModelCode       byte   `json:"model_code"`
	ModelReply      string `json:"model_reply_hex"`
	ParametersReply string `json:"parameters_reply_hex"`
}

func (s *Session) identify() (Identity, error) {
	var id Identity
	b, err := s.command("identify", 0x31, []byte{2}, 24, nil)
	if err != nil {
		return id, err
	}
	id.ModelCode, id.ModelReply = b[0], hex.EncodeToString(b)
	zero := byte(0)
	b, err = s.command("parameters", 0x53, []byte{0}, 24, &zero)
	if err != nil {
		return id, err
	}
	id.ParametersReply = hex.EncodeToString(b)
	return id, nil
}

func (s *Session) upload(p *Package, expected Identity) error {
	s.AcceptK = p.Profile == amzProfile
	// Re-identify in the same open session immediately before any download command.
	actual, err := s.identify()
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("Geräteantwort stimmt nicht mit dem Probebericht überein; kein Download gestartet")
	}
	ack := byte('O')
	startOptions := []byte{0, 0, 0}
	if p.Profile == amzProfile {
		// Frame 61 of the successful Studio capture sets all three download
		// flags. Zeroes received ACKs but did not install the project on AMZ043.
		startOptions = []byte{1, 1, 1}
	}
	if _, err := s.command("start", 0x34, startOptions, 1, &ack); err != nil {
		return err
	}
	s.Started = true
	if _, err := s.command("start-status", 0x35, nil, 1, &ack); err != nil {
		return err
	}
	for _, segment := range p.Segments {
		if _, err := s.command("segment:"+segment.Name, segment.Command, segment.Options, 1, &ack); err != nil {
			return err
		}
		if len(segment.Data) > 0 {
			if err := s.write("data", segment.Command, segment.Name, segment.Data, true); err != nil {
				return err
			}
		}
		if _, err := s.command("status:"+segment.Name, 0x35, nil, 1, &ack); err != nil {
			return err
		}
	}
	if _, err := s.command("commit", 0x1b, nil, 1, &ack); err != nil {
		return err
	}
	s.Committed = true
	if _, err := s.command("reset", 0x1a, nil, 0, nil); err != nil {
		return err
	}
	s.ResetWritten = true
	// A reboot may disconnect the USB endpoint. No legacy libusb error code is
	// assumed to have the same meaning under WinUSB. Commit ACK + reset write are
	// reported, not an unverified claim about screen/touch or nonvolatile storage.
	return nil
}

func (s *Session) status() string {
	if s.ResetWritten {
		return "commit_confirmed_reset_sent"
	}
	if s.Committed {
		return "commit_confirmed_reset_unconfirmed"
	}
	if s.CommitAttempted {
		return "commit_status_unknown"
	}
	if s.Started {
		return "partial_transfer_no_commit_confirmation"
	}
	if s.StartAttempted {
		return "download_start_status_unknown"
	}
	return "no_download_confirmed"
}

func checkTarget(p *Package, expectedModel string, id Identity, experimental bool) error {
	if p.SourceModel != expectedModel {
		return fmt.Errorf("Projektmodell %q stimmt nicht mit --model %q überein", p.SourceModel, expectedModel)
	}
	if p.SourceModel == "AMZ043W01" && id.ModelCode != 60 {
		return fmt.Errorf("AMZ043W01 benötigt laut Hersteller Gerätecode 60 (0x3c), erhalten 0x%02x", id.ModelCode)
	}
	// Only this model/code association is documented by the independent source.
	if p.SourceModel == "AMT070W124-L800X480R" && id.ModelCode == 0x4c {
		return nil
	}
	if !experimental {
		return fmt.Errorf("Modell %s / Gerätecode 0x%02x noch nicht hardwareverifiziert; AMZ-Erprobung erfordert --experimental", p.SourceModel, id.ModelCode)
	}
	return nil
}
