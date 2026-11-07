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

const (
	receiptMaxReceiptedMessageIDOctets   = 65
	receiptMaxAdditionalStatusInfoOctets = 256
)

var (
	// ErrNotDeliveryReceipt means there is no recognizable receipt metadata.
	ErrNotDeliveryReceipt = errors.New("smpp message: no delivery receipt metadata")
	// ErrInvalidDeliveryReceipt means the receipt has malformed or ambiguous
	// metadata that must not be silently used for message-ID correlation.
	ErrInvalidDeliveryReceipt = errors.New("smpp message: invalid delivery receipt")
	// ErrUnsupportedDeliveryReceiptDataCoding means a recognized receipt carries
	// text in a data_coding value this parser does not safely decode.
	ErrUnsupportedDeliveryReceiptDataCoding = errors.New("smpp message: unsupported delivery receipt data_coding")
)

// DeliveryReceipt is a parsed view of one inbound deliver_sm. IDs remain
// opaque, case-sensitive, and unchanged (including leading zeroes). Text
// counts, dates, error codes and the trailing text field are kept as received;
// the parser does not infer numeric or timestamp formats from vendor strings.
//
// MessageID favors the text id: field over receipted_message_id TLV. State
// independently favors message_state TLV over text stat:. When both disagree,
// each original value remains available alongside the conflict flags.
//
// State values use the SMPP 3.4 message_state wire numbers. Use
// SMPPMessageState for a typed protocol.MessageState view of the selected value.
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

	SubmittedCount string
	DeliveredCount string
	SubmitDate     string
	DoneDate       string
	ErrorCode      string
	Text           string

	NetworkErrorCode            [3]byte
	HasNetworkErrorCode         bool
	DeliveryFailureReason       uint8
	HasDeliveryFailureReason    bool
	AdditionalStatusInfoText    string
	HasAdditionalStatusInfoText bool
}

// SMPPMessageState returns the selected standard SMPP message_state as its
// protocol type. It returns false when no standard state is available.
func (r DeliveryReceipt) SMPPMessageState() (protocol.MessageState, bool) {
	if !r.HasState || r.State < uint8(protocol.MessageStateEnroute) || r.State > uint8(protocol.MessageStateRejected) {
		return 0, false
	}
	return protocol.MessageState(r.State), true
}

// ParseDeliveryReceipt extracts standard delivery-receipt metadata from one
// decoded protocol.DeliverSM. It does not acknowledge the PDU, correlate
// messages, normalize vendor-specific IDs, or infer delivery from submit_sm_resp.
//
// Text metadata supports the conventional ASCII-compatible receipt template
// in short_message, or message_payload when short_message is empty. Supported
// text data_coding values are SMSC default, IA5, Latin-1 and strict UCS-2/BMP.
// Packed GSM7 and other national alphabets are deliberately not guessed.
//
// Text id: takes precedence over receipted_message_id. For status, a valid
// message_state TLV takes precedence over a recognized stat: value. All
// original source values and conflicts remain available to the caller.
//
// Malformed/contradictory standard TLVs or text fields return
// ErrInvalidDeliveryReceipt. A recognized receipt using an unsupported text
// data_coding returns ErrUnsupportedDeliveryReceiptDataCoding. An ordinary
// deliver_sm without recognizable receipt evidence returns
// ErrNotDeliveryReceipt.
func ParseDeliveryReceipt(deliver protocol.DeliverSM) (DeliveryReceipt, error) {
	var receipt DeliveryReceipt
	var payload []byte
	var payloadSeen bool
	hasTLVReceipt := false

	for _, item := range deliver.Optional {
		switch item.Tag {
		case protocol.TLVTagReceiptedMessageID:
			id, err := receiptTLVCString(item.Value, "receipted_message_id", receiptMaxReceiptedMessageIDOctets, false)
			if err != nil {
				return DeliveryReceipt{}, err
			}
			if receipt.TLVMessageID != "" && receipt.TLVMessageID != id {
				return DeliveryReceipt{}, fmt.Errorf("%w: conflicting receipted_message_id TLVs", ErrInvalidDeliveryReceipt)
			}
			receipt.TLVMessageID = id
			hasTLVReceipt = true

		case protocol.TLVTagMessageState:
			if len(item.Value) != 1 || item.Value[0] < byte(protocol.MessageStateEnroute) || item.Value[0] > byte(protocol.MessageStateRejected) {
				return DeliveryReceipt{}, fmt.Errorf("%w: message_state TLV must be one octet in 1..8", ErrInvalidDeliveryReceipt)
			}
			if receipt.HasTLVState && receipt.TLVState != item.Value[0] {
				return DeliveryReceipt{}, fmt.Errorf("%w: conflicting message_state TLVs", ErrInvalidDeliveryReceipt)
			}
			receipt.TLVState = item.Value[0]
			receipt.HasTLVState = true
			hasTLVReceipt = true

		case protocol.TLVTagNetworkErrorCode:
			if len(item.Value) != len(receipt.NetworkErrorCode) {
				return DeliveryReceipt{}, fmt.Errorf("%w: network_error_code TLV must be exactly 3 octets", ErrInvalidDeliveryReceipt)
			}
			var code [3]byte
			copy(code[:], item.Value)
			if receipt.HasNetworkErrorCode && receipt.NetworkErrorCode != code {
				return DeliveryReceipt{}, fmt.Errorf("%w: conflicting network_error_code TLVs", ErrInvalidDeliveryReceipt)
			}
			receipt.NetworkErrorCode = code
			receipt.HasNetworkErrorCode = true
			hasTLVReceipt = true

		case protocol.TLVTagDeliveryFailureReason:
			if len(item.Value) != 1 {
				return DeliveryReceipt{}, fmt.Errorf("%w: delivery_failure_reason TLV must be exactly one octet", ErrInvalidDeliveryReceipt)
			}
			if receipt.HasDeliveryFailureReason && receipt.DeliveryFailureReason != item.Value[0] {
				return DeliveryReceipt{}, fmt.Errorf("%w: conflicting delivery_failure_reason TLVs", ErrInvalidDeliveryReceipt)
			}
			receipt.DeliveryFailureReason = item.Value[0]
			receipt.HasDeliveryFailureReason = true
			// SMPP 3.4 defines delivery_failure_reason for data_sm_resp,
			// not as evidence that this deliver_sm is a delivery receipt.

		case protocol.TLVTagAdditionalStatusInfoText:
			value, err := receiptTLVCString(item.Value, "additional_status_info_text", receiptMaxAdditionalStatusInfoOctets, true)
			if err != nil {
				return DeliveryReceipt{}, err
			}
			if receipt.HasAdditionalStatusInfoText && receipt.AdditionalStatusInfoText != value {
				return DeliveryReceipt{}, fmt.Errorf("%w: conflicting additional_status_info_text TLVs", ErrInvalidDeliveryReceipt)
			}
			receipt.AdditionalStatusInfoText = value
			receipt.HasAdditionalStatusInfoText = true
			// additional_status_info_text describes a response PDU. Retain it
			// when a peer supplies it, but do not use it to identify a receipt.

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
	markedReceipt := deliver.ESMClass&0x3c == 0x04

	var text string
	if len(raw) > 0 {
		switch deliver.DataCoding {
		case protocol.DataCodingSMSCDefault:
			// Conventional DLR templates commonly use ASCII-compatible octets.
			// Do not guess packed GSM7 because the septet count/padding is not
			// independently represented by short_message length.
			text = string(raw)

		case protocol.DataCodingIA5:
			for _, octet := range raw {
				if octet > 0x7f {
					return DeliveryReceipt{}, fmt.Errorf("%w: non-IA5 octet in receipt text", ErrInvalidDeliveryReceipt)
				}
			}
			text = string(raw)

		case protocol.DataCodingLatin1:
			text = decodeLatin1(raw)

		case protocol.DataCodingUCS2:
			decoded, err := smppenc.DecodeUCS2(raw)
			if err != nil {
				return DeliveryReceipt{}, fmt.Errorf("%w: invalid UCS-2 receipt text: %v", ErrInvalidDeliveryReceipt, err)
			}
			text = decoded

		default:
			if markedReceipt || hasTLVReceipt {
				return DeliveryReceipt{}, fmt.Errorf("%w: 0x%02x", ErrUnsupportedDeliveryReceiptDataCoding, uint8(deliver.DataCoding))
			}
		}
	}

	fields, err := extractReceiptTextFields(text)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	receipt.TextMessageID = fields.MessageID
	receipt.TextStatus = fields.Status
	receipt.SubmittedCount = fields.SubmittedCount
	receipt.DeliveredCount = fields.DeliveredCount
	receipt.SubmitDate = fields.SubmitDate
	receipt.DoneDate = fields.DoneDate
	receipt.ErrorCode = fields.ErrorCode
	receipt.Text = fields.Text

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

	textReceipt := receipt.TextMessageID != "" && receipt.TextStatus != ""
	if !markedReceipt && !hasTLVReceipt && !textReceipt {
		return DeliveryReceipt{}, ErrNotDeliveryReceipt
	}
	hasTextFields := receipt.TextMessageID != "" || receipt.SubmittedCount != "" ||
		receipt.DeliveredCount != "" || receipt.SubmitDate != "" || receipt.DoneDate != "" ||
		receipt.TextStatus != "" || receipt.ErrorCode != "" || receipt.Text != ""
	if markedReceipt && !hasTLVReceipt && !hasTextFields {
		return DeliveryReceipt{}, fmt.Errorf("%w: receipt marker has no recognizable fields", ErrInvalidDeliveryReceipt)
	}
	return receipt, nil
}

type receiptTextFields struct {
	MessageID      string
	SubmittedCount string
	DeliveredCount string
	SubmitDate     string
	DoneDate       string
	Status         string
	ErrorCode      string
	Text           string
}

func extractReceiptTextFields(text string) (receiptTextFields, error) {
	var fields receiptTextFields

	// text: is a free-form trailing field. Split it before tokenizing metadata
	// so repeated spaces, tabs and trailing whitespace in its value survive.
	metadata, textValue, hasText := splitReceiptText(text)
	if hasText {
		fields.Text = textValue
	}

	words := strings.Fields(metadata)
	for i := 0; i < len(words); i++ {
		word := words[i]
		switch {
		case hasPrefixFold(word, "id:"):
			value, next := consumeReceiptValue(words, i, word[len("id:"):])
			if err := setReceiptTextField(&fields.MessageID, value, "id"); err != nil {
				return receiptTextFields{}, err
			}
			i = next

		case hasPrefixFold(word, "sub:"):
			value, next := consumeReceiptValue(words, i, word[len("sub:"):])
			if err := setReceiptTextField(&fields.SubmittedCount, value, "sub"); err != nil {
				return receiptTextFields{}, err
			}
			i = next

		case hasPrefixFold(word, "dlvrd:"):
			value, next := consumeReceiptValue(words, i, word[len("dlvrd:"):])
			if err := setReceiptTextField(&fields.DeliveredCount, value, "dlvrd"); err != nil {
				return receiptTextFields{}, err
			}
			i = next

		case hasPrefixFold(word, "stat:"):
			value, next := consumeReceiptValue(words, i, word[len("stat:"):])
			if err := setReceiptTextField(&fields.Status, value, "stat"); err != nil {
				return receiptTextFields{}, err
			}
			i = next

		case hasPrefixFold(word, "err:"):
			value, next := consumeReceiptValue(words, i, word[len("err:"):])
			if err := setReceiptTextField(&fields.ErrorCode, value, "err"); err != nil {
				return receiptTextFields{}, err
			}
			i = next

		case strings.EqualFold(word, "submit") && i+1 < len(words) && hasPrefixFold(words[i+1], "date:"):
			value, next := consumeReceiptValue(words, i+1, words[i+1][len("date:"):])
			if err := setReceiptTextField(&fields.SubmitDate, value, "submit date"); err != nil {
				return receiptTextFields{}, err
			}
			i = next

		case strings.EqualFold(word, "done") && i+1 < len(words) && hasPrefixFold(words[i+1], "date:"):
			value, next := consumeReceiptValue(words, i+1, words[i+1][len("date:"):])
			if err := setReceiptTextField(&fields.DoneDate, value, "done date"); err != nil {
				return receiptTextFields{}, err
			}
			i = next
		}
	}
	return fields, nil
}

func hasPrefixFold(value, prefix string) bool {
	return len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix)
}

// splitReceiptText separates the first text: marker at a token boundary and
// removes exactly one delimiter whitespace byte after the colon. The rest of
// the free-form payload is returned byte-for-byte.
func splitReceiptText(value string) (metadata, textValue string, found bool) {
	const marker = "text:"
	for i := 0; i+len(marker) <= len(value); i++ {
		if i > 0 && !isReceiptWhitespace(value[i-1]) {
			continue
		}
		if !strings.EqualFold(value[i:i+len(marker)], marker) {
			continue
		}
		start := i + len(marker)
		if start < len(value) && isReceiptWhitespace(value[start]) {
			start++
		}
		return value[:i], value[start:], true
	}
	return value, "", false
}

func isReceiptWhitespace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

func isReceiptFieldStart(words []string, index int) bool {
	word := words[index]
	if hasPrefixFold(word, "id:") || hasPrefixFold(word, "sub:") ||
		hasPrefixFold(word, "dlvrd:") || hasPrefixFold(word, "stat:") ||
		hasPrefixFold(word, "err:") {
		return true
	}
	return (strings.EqualFold(word, "submit") || strings.EqualFold(word, "done")) &&
		index+1 < len(words) && hasPrefixFold(words[index+1], "date:")
}

func consumeReceiptValue(words []string, index int, inline string) (string, int) {
	if inline != "" || index+1 >= len(words) || isReceiptFieldStart(words, index+1) {
		return inline, index
	}
	return words[index+1], index + 1
}

func setReceiptTextField(destination *string, value, field string) error {
	if value == "" {
		return nil
	}
	if *destination != "" && *destination != value {
		return fmt.Errorf("%w: conflicting text %s fields", ErrInvalidDeliveryReceipt, field)
	}
	*destination = value
	return nil
}

func receiptTLVCString(value []byte, field string, maxOctets int, allowEmpty bool) (string, error) {
	if len(value) == 0 {
		return "", fmt.Errorf("%w: empty %s TLV", ErrInvalidDeliveryReceipt, field)
	}
	if len(value) > maxOctets {
		return "", fmt.Errorf("%w: %s C-Octet value exceeds %d octets", ErrInvalidDeliveryReceipt, field, maxOctets)
	}
	if value[len(value)-1] != 0 {
		return "", fmt.Errorf("%w: unterminated %s C-Octet value", ErrInvalidDeliveryReceipt, field)
	}
	value = value[:len(value)-1]
	if (len(value) == 0 && !allowEmpty) || bytes.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("%w: malformed %s C-Octet value", ErrInvalidDeliveryReceipt, field)
	}
	// C-Octet values are retained as opaque octets. Message IDs are not
	// restricted to UTF-8 or normalized to an assumed character set.
	return string(value), nil
}

func decodeLatin1(data []byte) string {
	var out strings.Builder
	out.Grow(len(data) * 2)
	for _, octet := range data {
		out.WriteRune(rune(octet))
	}
	return out.String()
}

func receiptTextState(status string) (uint8, bool) {
	switch {
	case strings.EqualFold(status, "ENROUTE"):
		return uint8(protocol.MessageStateEnroute), true
	case strings.EqualFold(status, "DELIVRD"):
		return uint8(protocol.MessageStateDelivered), true
	case strings.EqualFold(status, "EXPIRED"):
		return uint8(protocol.MessageStateExpired), true
	case strings.EqualFold(status, "DELETED"):
		return uint8(protocol.MessageStateDeleted), true
	case strings.EqualFold(status, "UNDELIV"):
		return uint8(protocol.MessageStateUndeliverable), true
	case strings.EqualFold(status, "ACCEPTD"):
		return uint8(protocol.MessageStateAccepted), true
	case strings.EqualFold(status, "UNKNOWN"):
		return uint8(protocol.MessageStateUnknown), true
	case strings.EqualFold(status, "REJECTD"):
		return uint8(protocol.MessageStateRejected), true
	default:
		return 0, false
	}
}
