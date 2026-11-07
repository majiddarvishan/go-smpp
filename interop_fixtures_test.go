package smpp_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/message"
	"github.com/majiddarvishan/go-smpp/protocol"
)

const interopManifestPath = "testdata/interop/manifest.json"

type interopManifest struct {
	SchemaVersion int              `json:"schema_version"`
	Fixtures      []interopFixture `json:"fixtures"`
}

type interopFixture struct {
	ID              string               `json:"id"`
	Source          string               `json:"source"`
	PeerAlias       string               `json:"peer_alias"`
	ProtocolVersion string               `json:"protocol_version"`
	CaptureMethod   string               `json:"capture_method"`
	CapturedAt      string               `json:"captured_at,omitempty"`
	EvidenceRef     string               `json:"evidence_ref,omitempty"`
	Sanitization    *interopSanitization `json:"sanitization,omitempty"`
	Frames          []interopFrame       `json:"frames"`
}

type interopSanitization struct {
	Reviewed                bool   `json:"reviewed"`
	Policy                  string `json:"policy"`
	BindFramesExcluded      bool   `json:"bind_frames_excluded"`
	SensitiveValuesReplaced bool   `json:"sensitive_values_replaced"`
	UnknownTLVs             string `json:"unknown_tlvs"`
}

type interopFrame struct {
	Direction string          `json:"direction"`
	Hex       string          `json:"hex"`
	Expect    interopExpected `json:"expect"`
}

type interopExpected struct {
	Outcome        string                  `json:"outcome"`
	CommandID      string                  `json:"command_id"`
	CommandStatus  string                  `json:"command_status"`
	SequenceNumber uint32                  `json:"sequence_number"`
	BodyType       string                  `json:"body_type,omitempty"`
	ErrorKind      string                  `json:"error_kind,omitempty"`
	Reencode       string                  `json:"reencode,omitempty"`
	Receipt        *interopReceiptExpected `json:"receipt,omitempty"`
}

type interopReceiptExpected struct {
	MessageID  string `json:"message_id"`
	TextStatus string `json:"text_status"`
}

var (
	interopFixtureIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,63}$`)
	interopPeerAliasPattern = regexp.MustCompile(`^peer-[0-9]{3}$`)
	interopEvidencePattern  = regexp.MustCompile(`^INT-01-PEER-[0-9]{3}-[0-9]{3}$`)
)

func TestInteroperabilityFixtures(t *testing.T) {
	manifest, err := loadInteropManifest(interopManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateInteropManifest(manifest); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range manifest.Fixtures {
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			registry, err := interopRegistry(fixture.ProtocolVersion)
			if err != nil {
				t.Fatal(err)
			}

			byDirection := map[string][][]byte{
				"esme_to_smsc": nil,
				"smsc_to_esme": nil,
			}
			for index, item := range fixture.Frames {
				raw, err := hex.DecodeString(item.Hex)
				if err != nil {
					t.Fatalf("frame %d: decode hex: %v", index, err)
				}
				decoded, decodeErr := codec.DecodePDU(raw, registry)
				if item.Expect.Outcome == "reject" {
					if decodeErr == nil {
						t.Fatalf("frame %d: expected %s decode rejection", index, item.Expect.ErrorKind)
					}
					assertInteropErrorKind(t, decodeErr, item.Expect.ErrorKind)
					continue
				}
				if decodeErr != nil {
					t.Fatalf("frame %d: decode PDU: %v", index, decodeErr)
				}

				if uint32(decoded.Header.CommandID) != interopParseHex32(t, item.Expect.CommandID) ||
					uint32(decoded.Header.CommandStatus) != interopParseHex32(t, item.Expect.CommandStatus) ||
					uint32(decoded.Header.SequenceNumber) != item.Expect.SequenceNumber {
					t.Fatalf("frame %d: decoded header does not match fixture expectation: %+v", index, decoded.Header)
				}
				if got := fmt.Sprintf("%T", decoded.Body); got != item.Expect.BodyType {
					t.Fatalf("frame %d: body type = %q, want %q", index, got, item.Expect.BodyType)
				}

				if item.Expect.Reencode == "exact" {
					encoded, err := codec.EncodePDU(nil, decoded.Header, decoded.Body, registry)
					if err != nil {
						t.Fatalf("frame %d: re-encode: %v", index, err)
					}
					if !bytes.Equal(encoded, raw) {
						t.Fatalf("frame %d: exact wire round-trip mismatch\n got: %x\nwant: %x", index, encoded, raw)
					}
				}

				if expected := item.Expect.Receipt; expected != nil {
					deliver, ok := decoded.Body.(protocol.DeliverSM)
					if !ok {
						t.Fatalf("frame %d: receipt expectation requires protocol.DeliverSM, got %T", index, decoded.Body)
					}
					receipt, err := message.ParseDeliveryReceipt(deliver)
					if err != nil {
						t.Fatalf("frame %d: parse receipt: %v", index, err)
					}
					if receipt.MessageID != expected.MessageID || receipt.TextStatus != expected.TextStatus {
						t.Fatalf("frame %d: receipt = (%q, %q), want (%q, %q)",
							index, receipt.MessageID, receipt.TextStatus, expected.MessageID, expected.TextStatus)
					}
				}

				byDirection[item.Direction] = append(byDirection[item.Direction], raw)
			}

			for _, direction := range []string{"esme_to_smsc", "smsc_to_esme"} {
				frames := byDirection[direction]
				if len(frames) == 0 {
					continue
				}
				t.Run(direction+"_stream", func(t *testing.T) {
					replayInteropStream(t, frames)
				})
			}
		})
	}
}

func TestInteropFixtureValidationRequiresReviewedRealPeerEvidence(t *testing.T) {
	manifest, err := loadInteropManifest(interopManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture := manifest.Fixtures[0]
	fixture.Source = "real-peer-anonymized"
	fixture.PeerAlias = "peer-001"
	fixture.CaptureMethod = "wire-capture"
	fixture.CapturedAt = "2026-11-07T10:00:00Z"
	fixture.EvidenceRef = "INT-01-PEER-001-001"
	fixture.Sanitization = nil

	if err := validateInteropFixture(fixture); err == nil {
		t.Fatal("unreviewed real-peer evidence was accepted")
	}
}

func TestInteropFixtureValidationRejectsBindCredentialFrames(t *testing.T) {
	manifest, err := loadInteropManifest(interopManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture := manifest.Fixtures[0]
	fixture.Frames = append(fixture.Frames, interopFrame{
		Direction: "esme_to_smsc",
		Hex:       "0000001000000002000000000000000a",
		Expect: interopExpected{
			Outcome: "accept", CommandID: "0x00000002", CommandStatus: "0x00000000",
			SequenceNumber: 10, BodyType: "protocol.BindRequest", Reencode: "exact",
		},
	})
	if err := validateInteropFixture(fixture); err == nil {
		t.Fatal("bind_transmitter frame was accepted into persistent testdata")
	}
}

func loadInteropManifest(path string) (interopManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return interopManifest{}, fmt.Errorf("read interoperability manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var manifest interopManifest
	if err := decoder.Decode(&manifest); err != nil {
		return interopManifest{}, fmt.Errorf("decode interoperability manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return interopManifest{}, errors.New("interoperability manifest contains trailing JSON")
		}
		return interopManifest{}, fmt.Errorf("read trailing manifest data: %w", err)
	}
	return manifest, nil
}

func validateInteropManifest(manifest interopManifest) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported interoperability manifest schema_version %d", manifest.SchemaVersion)
	}
	if len(manifest.Fixtures) == 0 {
		return errors.New("interoperability manifest contains no fixtures")
	}
	seen := make(map[string]struct{}, len(manifest.Fixtures))
	for _, fixture := range manifest.Fixtures {
		if _, exists := seen[fixture.ID]; exists {
			return fmt.Errorf("duplicate interoperability fixture id %q", fixture.ID)
		}
		seen[fixture.ID] = struct{}{}
		if err := validateInteropFixture(fixture); err != nil {
			return fmt.Errorf("fixture %q: %w", fixture.ID, err)
		}
	}
	return nil
}

func validateInteropFixture(fixture interopFixture) error {
	if !interopFixtureIDPattern.MatchString(fixture.ID) {
		return errors.New("id must be a lowercase slug of 3..64 characters")
	}
	if fixture.ProtocolVersion != "3.4" && fixture.ProtocolVersion != "5.0" {
		return fmt.Errorf("unsupported protocol_version %q", fixture.ProtocolVersion)
	}
	if len(fixture.Frames) == 0 {
		return errors.New("fixture contains no frames")
	}

	switch fixture.Source {
	case "synthetic":
		if fixture.PeerAlias != "synthetic" || fixture.CaptureMethod != "constructed" {
			return errors.New("synthetic fixtures require peer_alias=synthetic and capture_method=constructed")
		}
		if fixture.CapturedAt != "" || fixture.EvidenceRef != "" || fixture.Sanitization != nil {
			return errors.New("synthetic fixtures must not claim capture provenance or real-peer sanitization")
		}
	case "real-peer-anonymized":
		if !interopPeerAliasPattern.MatchString(fixture.PeerAlias) {
			return errors.New("real-peer fixtures require an opaque peer alias such as peer-001")
		}
		if fixture.CaptureMethod != "wire-capture" && fixture.CaptureMethod != "manual-transcription" {
			return errors.New("real-peer capture_method must be wire-capture or manual-transcription")
		}
		if _, err := time.Parse(time.RFC3339, fixture.CapturedAt); err != nil {
			return fmt.Errorf("real-peer captured_at must be RFC3339: %w", err)
		}
		if !interopEvidencePattern.MatchString(fixture.EvidenceRef) {
			return errors.New("real-peer evidence_ref must be an opaque INT-01 reference")
		}
		sanitization := fixture.Sanitization
		if sanitization == nil || !sanitization.Reviewed ||
			sanitization.Policy != "peer-aware-allowlist-v1" ||
			!sanitization.BindFramesExcluded ||
			!sanitization.SensitiveValuesReplaced ||
			(sanitization.UnknownTLVs != "removed" && sanitization.UnknownTLVs != "reviewed-safe") {
			return errors.New("real-peer fixtures require reviewed peer-aware sanitization, excluded bind frames, replaced sensitive values, and removed or reviewed-safe unknown TLVs")
		}
	default:
		return fmt.Errorf("unsupported source %q", fixture.Source)
	}

	for index, item := range fixture.Frames {
		if item.Direction != "esme_to_smsc" && item.Direction != "smsc_to_esme" {
			return fmt.Errorf("frame %d has invalid direction %q", index, item.Direction)
		}
		if item.Hex == "" {
			return fmt.Errorf("frame %d has empty hex data", index)
		}
		raw, err := hex.DecodeString(item.Hex)
		if err != nil {
			return fmt.Errorf("frame %d has invalid hex data: %w", index, err)
		}
		if len(raw) < codec.HeaderSize {
			return fmt.Errorf("frame %d is shorter than the SMPP header", index)
		}

		commandID := protocol.CommandID(binary.BigEndian.Uint32(raw[4:8]))
		if isInteropCredentialCommand(commandID) {
			return fmt.Errorf("frame %d contains a bind/outbind command, which must not be persisted", index)
		}
		expectedCommandID, err := parseInteropHex32(item.Expect.CommandID)
		if err != nil {
			return fmt.Errorf("frame %d command_id: %w", index, err)
		}
		expectedStatus, err := parseInteropHex32(item.Expect.CommandStatus)
		if err != nil {
			return fmt.Errorf("frame %d command_status: %w", index, err)
		}
		if uint32(commandID) != expectedCommandID ||
			binary.BigEndian.Uint32(raw[8:12]) != expectedStatus ||
			binary.BigEndian.Uint32(raw[12:16]) != item.Expect.SequenceNumber {
			return fmt.Errorf("frame %d raw header does not match declared expectation", index)
		}
		if item.Expect.SequenceNumber == 0 {
			return fmt.Errorf("frame %d sequence_number must be non-zero", index)
		}

		switch item.Expect.Outcome {
		case "accept":
			if binary.BigEndian.Uint32(raw[:4]) != uint32(len(raw)) {
				return fmt.Errorf("frame %d accepted outcome must have command_length equal to frame size", index)
			}
			if item.Expect.BodyType == "" || item.Expect.ErrorKind != "" {
				return fmt.Errorf("frame %d accepted outcome requires body_type and forbids error_kind", index)
			}
			if item.Expect.Reencode != "exact" && item.Expect.Reencode != "decode-only" {
				return fmt.Errorf("frame %d accepted outcome requires reencode=exact or decode-only", index)
			}
			if item.Expect.Receipt != nil && item.Expect.BodyType != "protocol.DeliverSM" {
				return fmt.Errorf("frame %d receipt expectation requires protocol.DeliverSM", index)
			}
			if item.Expect.Receipt != nil && (item.Expect.Receipt.MessageID == "" || item.Expect.Receipt.TextStatus == "") {
				return fmt.Errorf("frame %d receipt expectation requires message_id and text_status", index)
			}
		case "reject":
			if item.Expect.BodyType != "" || item.Expect.ErrorKind == "" || item.Expect.Reencode != "" || item.Expect.Receipt != nil {
				return fmt.Errorf("frame %d rejected outcome requires error_kind and forbids body_type/reencode/receipt", index)
			}
			if item.Expect.ErrorKind != "fatal" && item.Expect.ErrorKind != "semantic" && item.Expect.ErrorKind != "other" {
				return fmt.Errorf("frame %d has unsupported error_kind %q", index, item.Expect.ErrorKind)
			}
		default:
			return fmt.Errorf("frame %d has unsupported outcome %q", index, item.Expect.Outcome)
		}
	}
	return nil
}

func isInteropCredentialCommand(commandID protocol.CommandID) bool {
	switch commandID {
	case protocol.CommandBindReceiver, protocol.CommandBindReceiverResp,
		protocol.CommandBindTransmitter, protocol.CommandBindTransmitterResp,
		protocol.CommandBindTransceiver, protocol.CommandBindTransceiverResp,
		protocol.CommandOutbind:
		return true
	default:
		return false
	}
}

func parseInteropHex32(value string) (uint32, error) {
	if len(value) != 10 || !strings.HasPrefix(value, "0x") {
		return 0, fmt.Errorf("expected 0x-prefixed 8-digit hexadecimal value, got %q", value)
	}
	parsed, err := strconv.ParseUint(value[2:], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid hexadecimal value %q: %w", value, err)
	}
	return uint32(parsed), nil
}

func interopParseHex32(t *testing.T, value string) uint32 {
	t.Helper()
	parsed, err := parseInteropHex32(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func interopRegistry(version string) (*codec.Registry, error) {
	switch version {
	case "3.4":
		return codec.NewSMPP34Registry(codec.RegistryCompatible)
	case "5.0":
		return codec.NewSMPP50Registry(codec.RegistryCompatible)
	default:
		return nil, fmt.Errorf("unsupported protocol version %q", version)
	}
}

func assertInteropErrorKind(t *testing.T, err error, want string) {
	t.Helper()
	var fatal *protocol.FatalError
	var semantic *protocol.SemanticError
	switch want {
	case "fatal":
		if !errors.As(err, &fatal) {
			t.Fatalf("error type = %T, want protocol.FatalError", err)
		}
	case "semantic":
		if !errors.As(err, &semantic) {
			t.Fatalf("error type = %T, want protocol.SemanticError", err)
		}
	case "other":
		if errors.As(err, &fatal) || errors.As(err, &semantic) {
			t.Fatalf("error type = %T, want non-fatal/non-semantic error", err)
		}
	default:
		t.Fatalf("unsupported expected error kind %q", want)
	}
}

func replayInteropStream(t *testing.T, frames [][]byte) {
	t.Helper()
	framer, err := codec.NewFramer(0)
	if err != nil {
		t.Fatal(err)
	}

	var stream []byte
	for _, frame := range frames {
		stream = append(stream, frame...)
	}
	var got [][]byte
	chunkSizes := [...]int{1, 3, 2, 7, 16, 5, 11}
	chunkIndex := 0
	for offset := 0; offset < len(stream); {
		end := offset + chunkSizes[chunkIndex%len(chunkSizes)]
		if end > len(stream) {
			end = len(stream)
		}
		if err := framer.Feed(stream[offset:end], func(frame []byte) error {
			got = append(got, append([]byte(nil), frame...))
			return nil
		}); err != nil {
			t.Fatalf("feed fragmented/coalesced stream: %v", err)
		}
		offset = end
		chunkIndex++
	}
	if err := framer.Finalize(); err != nil {
		t.Fatalf("finalize stream: %v", err)
	}
	if len(got) != len(frames) {
		t.Fatalf("framer emitted %d frames, want %d", len(got), len(frames))
	}
	for index := range frames {
		if !bytes.Equal(got[index], frames[index]) {
			t.Fatalf("framed PDU %d mismatch\n got: %x\nwant: %x", index, got[index], frames[index])
		}
	}
}
