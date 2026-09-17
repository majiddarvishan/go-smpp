package protocol

import "testing"

func TestSMPP34CommandStatusValues(t *testing.T) {
	values := map[CommandStatus]uint32{
		StatusOK: 0x00000000, StatusInvalidMessageLength: 0x00000001, StatusInvalidCommandLength: 0x00000002,
		StatusInvalidCommandID: 0x00000003, StatusInvalidBindState: 0x00000004, StatusAlreadyBound: 0x00000005,
		StatusInvalidPriorityFlag: 0x00000006, StatusInvalidRegisteredDeliveryFlag: 0x00000007, StatusSystemError: 0x00000008,
		StatusInvalidSourceAddress: 0x0000000A, StatusInvalidDestinationAddress: 0x0000000B, StatusInvalidMessageID: 0x0000000C,
		StatusBindFailed: 0x0000000D, StatusInvalidPassword: 0x0000000E, StatusInvalidSystemID: 0x0000000F,
		StatusCancelFailed: 0x00000011, StatusReplaceFailed: 0x00000013, StatusMessageQueueFull: 0x00000014,
		StatusInvalidServiceType: 0x00000015, StatusInvalidNumberOfDestinations: 0x00000033, StatusInvalidDistributionListName: 0x00000034,
		StatusInvalidDestinationFlag: 0x00000040, StatusInvalidSubmitWithReplace: 0x00000042, StatusInvalidESMClass: 0x00000043,
		StatusCannotSubmitToDistributionList: 0x00000044, StatusSubmitFailed: 0x00000045, StatusInvalidSourceTON: 0x00000048,
		StatusInvalidSourceNPI: 0x00000049, StatusInvalidDestinationTON: 0x00000050, StatusInvalidDestinationNPI: 0x00000051,
		StatusInvalidSystemType: 0x00000053, StatusInvalidReplaceFlag: 0x00000054, StatusInvalidNumberOfMessages: 0x00000055,
		StatusThrottled: 0x00000058, StatusInvalidScheduledDeliveryTime: 0x00000061, StatusInvalidValidityPeriod: 0x00000062,
		StatusInvalidDefaultMessageID: 0x00000063, StatusReceiverTemporaryAppError: 0x00000064, StatusReceiverPermanentAppError: 0x00000065,
		StatusReceiverRejectMessage: 0x00000066, StatusQueryFailed: 0x00000067, StatusInvalidOptionalParameterStream: 0x000000C0,
		StatusOptionalParameterNotAllowed: 0x000000C1, StatusInvalidParameterLength: 0x000000C2, StatusMissingOptionalParameter: 0x000000C3,
		StatusInvalidOptionalParameterValue: 0x000000C4, StatusDeliveryFailure: 0x000000FE, StatusUnknownError: 0x000000FF,
	}
	if len(values) != 48 {
		t.Fatalf("named SMPP 3.4 status matrix has %d entries, want 48", len(values))
	}
	for got, want := range values {
		if uint32(got) != want {
			t.Fatalf("status 0x%08x != expected 0x%08x", uint32(got), want)
		}
	}
}

func TestSMPP34MessageStateValues(t *testing.T) {
	values := []struct {
		got  MessageState
		want uint8
	}{
		{MessageStateEnroute, 1}, {MessageStateDelivered, 2}, {MessageStateExpired, 3}, {MessageStateDeleted, 4},
		{MessageStateUndeliverable, 5}, {MessageStateAccepted, 6}, {MessageStateUnknown, 7}, {MessageStateRejected, 8},
	}
	for _, tc := range values {
		if uint8(tc.got) != tc.want {
			t.Fatalf("message state=%d want %d", tc.got, tc.want)
		}
	}
}

func TestSMPP34DestinationFlags(t *testing.T) {
	if DestinationSME != 1 || DestinationDistributionList != 2 {
		t.Fatalf("destination flags SME=%d DL=%d", DestinationSME, DestinationDistributionList)
	}
}
