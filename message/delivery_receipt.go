package message

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	smppenc "github.com/majiddarvishan/go-smpp/encoding"
	"github.com/majiddarvishan/go-smpp/protocol"
)

// ReceiptFieldSource reports where a delivery-receipt field was selected.
// ReceiptSourceNone is used when no valid field exists.
type ReceiptFieldSource string

const (
	ReceiptSourceNone ReceiptFieldSource = ""
	ReceiptSourceText ReceiptFieldSource = "text"
	ReceiptSourceTLV  ReceiptFieldSource = "tlv"
)

var (
	// ErrNotDeliveryReceipt means there is no recognizable receipt metadata.
	ErrNotDeliveryReceipt = errors.New("smpp message: no delivery receipt metadata")
	// ErrInvalidDeliveryReceipt means the receipt has malformed or ambiguous
	// metadata that must not be silently used for message-ID correlation.
	ErrInvalidDeliveryReceipt = errors.New("smpp message: invalid delivery receipt")
)

// DeliveryReceipt is a parsed view of one inbound deliver_sm. IDs remain
// opaque, case-sensitive, and unchanged (including leading zeroes).
//
// MessageID favors the text id: field over receipted_message_id TLV. State
// independently favors message_state TLV over text stat:. When both disagree,
// each original value remains available alongside the conflict flags.
//
// State values use the SMPP 3.4 message_state wire numbers:
// 1=ENROUTE, 2=DELIVERED, 3=EXPIRED, 4=DELETED, 5=UNDELIVERABLE,
// 6=ACCEPTED, 7=UNKNOWN, 8=REJECTED. A text-only unrecognized stat:
// remains in TextStatus without inventing a numeric state.
//
// This value contains no references to borrowed decoder slices.
type DeliveryReceipt struct {
	MessageID         string
	MessageIDSource   ReceiptFieldSource
	TextMessageID     string
	TLVMessageID      string
	MessageIDConflict bool

	State         uint8
	HasState      bool
	StateSource   ReceiptFieldSource
	TextStatus    string
	TextState     uint8
	HasTextState  bool
	TLVState      uint8
	HasTLVState   bool
	StateConflict bool
}

// ParseDeliveryReceipt extracts receipt metadata without changing the original
// SMPP PDU or acknowledging it. It does not correlate messages, normalize
// vendor-specific IDs, or infer a delivery result from submit_sm_resp.
//
// It supports the conventional ASCII fields in short_message (or the
// message_payload TLV when short_message is empty), and strict UCS-2 for
// data_coding=0x08. The text: field ends metadata parsing: id:/stat: inside
// echoed message text are not treated as receipt metadata.
//
// Conflicting duplicate TLVs or text fields, malformed TLV values and invalid
// message_state values return ErrInvalidDeliveryReceipt instead of guessing.
// Duplicate identical TLVs are tolerated. An ordinary deliver_sm without any
// receipt metadata returns ErrNotDeliveryReceipt, not a synthetic DLR.
func ParseDeliveryReceipt(deliver protocol.DeliverSM) (DeliveryReceipt, error) {
	var receipt DeliveryReceipt
	var payload []byte
	var payloadSeen bool
	for _, item := range deliver.Optional {
		switch item.Tag {
		case protocol.TLVTagReceiptedMessageID:
			id, err := receiptTLVMessageID(item.Value)
			if err != nil {
				return DeliveryReceipt{}, err
			}
			if receipt.TLVMessageID != "" && receipt.TLVMessageID != id {
				return DeliveryReceipt{}, fmt.Errorf("%w: conflicting receipted_message_id TLVs", ErrInvalidDeliveryReceipt)
			}
			receipt.TLVMessageID = id
		case protocol.TLVTagMessageState:
			if len(item.Value) != 1 || item.Value[0] < 1 || item.Value[0] > 8 {
				return DeliveryReceipt{}, fmt.Errorf("%w: message_state TLV must be one octet in 1..8", ErrInvalidDeliveryReceipt)
			}
			if receipt.HasTLVState && receipt.TLVState != item.Value[0] {
				return DeliveryReceipt{}, fmt.Errorf("%w: conflicting message_state TLVs", ErrInvalidDeliveryReceipt)
			}
			receipt.TLVState = item.Value[0]
			receipt.HasTLVState = true
		case protocol.TLVTagMessagePayload:
			if payloadSeen && !bytes.Equal(payload, item.Value) {
				return DeliveryReceipt{}, fmt.Errorf("%w: conflicting message_payload TLVs", ErrInvalidDeliveryReceipt)
			}
			payload = item.Value
			payloadSeen = true
		}
	}
	if payloadSeen && len(deliver.ShortMessage) > 0 {
		return DeliveryReceipt{}, fmt.Errorf("%w: short_message and message_payload both set", ErrInvalidDeliveryReceipt)
	}
	raw := deliver.ShortMessage
	if len(raw) == 0 && payloadSeen {
		raw = payload
	}
	var text string
	if len(raw) > 0 {
		if deliver.DataCoding == protocol.DataCodingUCS2 {
			var err error
			text, err = smppenc.DecodeUCS2(raw)
			if err != nil {
				return DeliveryReceipt{}, fmt.Errorf("%w: invalid UCS-2 receipt text: %v", ErrInvalidDeliveryReceipt, err)
			}
		} else if deliver.DataCoding == protocol.DataCodingSMSCDefault {
			// Conventional DLR templates carry ASCII field labels/values
			// directly as short_message octets. Do not guess whether a peer
			// packed septets or apply a lossy charset conversion.
			text = string(raw)
		}
	}
	var err error
	receipt.TextMessageID, receipt.TextStatus, err = extractReceiptTextFields(text)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	if receipt.TextMessageID != "" {
		receipt.MessageID = receipt.TextMessageID
		receipt.MessageIDSource = ReceiptSourceText
	} else if receipt.TLVMessageID != "" {
		receipt.MessageID = receipt.TLVMessageID
		receipt.MessageIDSource = ReceiptSourceTLV
	}
	receipt.MessageIDConflict = receipt.TextMessageID != "" && receipt.TLVMessageID != "" && receipt.TextMessageID != receipt.TLVMessageID

	if receipt.TextStatus != "" {
		receipt.TextState, receipt.HasTextState = receiptTextState(receipt.TextStatus)
	}
	if receipt.HasTLVState {
		receipt.State = receipt.TLVState
		receipt.HasState = true
		receipt.StateSource = ReceiptSourceTLV
	} else if receipt.HasTextState {
		receipt.State = receipt.TextState
		receipt.HasState = true
		receipt.StateSource = ReceiptSourceText
	}
	receipt.StateConflict = receipt.HasTextState && receipt.HasTLVState && receipt.TextState != receipt.TLVState

	// An ordinary SMS may happen to contain "id:123"; do not report it
	// as a delivery receipt unless the message is marked as an SMSC receipt,
	// a recognized stat: field is present, or a standard receipt TLV exists.
	markedReceipt := deliver.ESMClass&0x3c == 0x04
	hasTLVReceipt := receipt.TLVMessageID != "" || receipt.HasTLVState
	textReceipt := receipt.TextStatus != "" && (receipt.TextMessageID != "" || receipt.HasTextState)
	if !markedReceipt && !hasTLVReceipt && !textReceipt {
		return DeliveryReceipt{}, ErrNotDeliveryReceipt
	}
	if receipt.MessageID == "" && receipt.TextStatus == "" && !receipt.HasTLVState {
		return DeliveryReceipt{}, ErrNotDeliveryReceipt
	}
	return receipt, nil
}

func receiptTLVMessageID(value []byte) (string, error) {
	if len(value) == 0 {
		return "", fmt.Errorf("%w: empty receipted_message_id TLV", ErrInvalidDeliveryReceipt)
	}
	if value[len(value)-1] == 0 {
		value = value[:len(value)-1] // C-Octet NULL terminator is not part of ID
	}
	if len(value) == 0 || bytes.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("%w: malformed receipted_message_id C-Octet value", ErrInvalidDeliveryReceipt)
	}
	return string(value), nil
}

func extractReceiptTextFields(text string) (id, status string, err error) {
	words := strings.Fields(text)
	for i := 0; i < len(words); i++ {
		word := words[i]
		if len(word) >= 5 && strings.EqualFold(word[:5], "text:") {
			break // echoed original SMS content is not DLR metadata
		}
		var field string
		switch {
		case len(word) >= 3 && strings.EqualFold(word[:3], "id:"):
			field = "id"
			word = word[3:]
		case len(word) >= 5 && strings.EqualFold(word[:5], "stat:"):
			field = "stat"
			word = word[5:]
		default:
			continue
		}
		if word == "" && i+1 < len(words) && !strings.ContainsRune(words[i+1], ':') {
			i++
			word = words[i]
		}
		if word == "" {
			continue
		}
		switch field {
		case "id":
			if id != "" && id != word {
				return "", "", fmt.Errorf("%w: conflicting text id fields", ErrInvalidDeliveryReceipt)
			}
			id = word
		case "stat":
			if status != "" && status != word {
				return "", "", fmt.Errorf("%w: conflicting text stat fields", ErrInvalidDeliveryReceipt)
			}
			status = word
		}
	}
	return id, status, nil
}

func receiptTextState(status string) (uint8, bool) {
	switch {
	case strings.EqualFold(status, "ENROUTE"):
		return 1, true
	case strings.EqualFold(status, "DELIVRD"):
		return 2, true
	case strings.EqualFold(status, "EXPIRED"):
		return 3, true
	case strings.EqualFold(status, "DELETED"):
		return 4, true
	case strings.EqualFold(status, "UNDELIV"):
		return 5, true
	case strings.EqualFold(status, "ACCEPTD"):
		return 6, true
	case strings.EqualFold(status, "UNKNOWN"):
		return 7, true
	case strings.EqualFold(status, "REJECTD"):
		return 8, true
	default:
		return 0, false
	}
}
