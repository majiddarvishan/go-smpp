package protocol

// CommandStatus is the SMPP command_status value carried by response PDUs.
type CommandStatus uint32

// Standard SMPP command status values supported by the shared 3.4/5.0 core.
const (
	StatusOK                             CommandStatus = 0x00000000
	StatusInvalidMessageLength           CommandStatus = 0x00000001
	StatusInvalidCommandLength           CommandStatus = 0x00000002
	StatusInvalidCommandID               CommandStatus = 0x00000003
	StatusInvalidBindState               CommandStatus = 0x00000004
	StatusAlreadyBound                   CommandStatus = 0x00000005
	StatusInvalidPriorityFlag            CommandStatus = 0x00000006
	StatusInvalidRegisteredDeliveryFlag  CommandStatus = 0x00000007
	StatusSystemError                    CommandStatus = 0x00000008
	StatusInvalidSourceAddress           CommandStatus = 0x0000000A
	StatusInvalidDestinationAddress      CommandStatus = 0x0000000B
	StatusInvalidMessageID               CommandStatus = 0x0000000C
	StatusBindFailed                     CommandStatus = 0x0000000D
	StatusInvalidPassword                CommandStatus = 0x0000000E
	StatusInvalidSystemID                CommandStatus = 0x0000000F
	StatusCancelFailed                   CommandStatus = 0x00000011
	StatusReplaceFailed                  CommandStatus = 0x00000013
	StatusMessageQueueFull               CommandStatus = 0x00000014
	StatusInvalidServiceType             CommandStatus = 0x00000015
	StatusInvalidNumberOfDestinations    CommandStatus = 0x00000033
	StatusInvalidDistributionListName    CommandStatus = 0x00000034
	StatusInvalidDestinationFlag         CommandStatus = 0x00000040
	StatusInvalidSubmitWithReplace       CommandStatus = 0x00000042
	StatusInvalidESMClass                CommandStatus = 0x00000043
	StatusCannotSubmitToDistributionList CommandStatus = 0x00000044
	StatusSubmitFailed                   CommandStatus = 0x00000045
	StatusInvalidSourceTON               CommandStatus = 0x00000048
	StatusInvalidSourceNPI               CommandStatus = 0x00000049
	StatusInvalidDestinationTON          CommandStatus = 0x00000050
	StatusInvalidDestinationNPI          CommandStatus = 0x00000051
	StatusInvalidSystemType              CommandStatus = 0x00000053
	StatusInvalidReplaceFlag             CommandStatus = 0x00000054
	StatusInvalidNumberOfMessages        CommandStatus = 0x00000055
	StatusThrottled                      CommandStatus = 0x00000058
	StatusInvalidScheduledDeliveryTime   CommandStatus = 0x00000061
	StatusInvalidValidityPeriod          CommandStatus = 0x00000062
	StatusInvalidDefaultMessageID        CommandStatus = 0x00000063
	StatusReceiverTemporaryAppError      CommandStatus = 0x00000064
	StatusReceiverPermanentAppError      CommandStatus = 0x00000065
	StatusReceiverRejectMessage          CommandStatus = 0x00000066
	StatusQueryFailed                    CommandStatus = 0x00000067
	StatusInvalidOptionalParameterStream CommandStatus = 0x000000C0
	StatusOptionalParameterNotAllowed    CommandStatus = 0x000000C1
	StatusInvalidParameterLength         CommandStatus = 0x000000C2
	StatusMissingOptionalParameter       CommandStatus = 0x000000C3
	StatusInvalidOptionalParameterValue  CommandStatus = 0x000000C4
	StatusDeliveryFailure                CommandStatus = 0x000000FE
	StatusUnknownError                   CommandStatus = 0x000000FF
	StatusServiceTypeUnauthorized        CommandStatus = 0x00000100
	StatusProhibited                     CommandStatus = 0x00000101
	StatusServiceTypeUnavailable         CommandStatus = 0x00000102
	StatusServiceTypeDenied              CommandStatus = 0x00000103
	StatusInvalidDataCodingScheme        CommandStatus = 0x00000104
	StatusInvalidSourceAddrSubunit       CommandStatus = 0x00000105
	StatusInvalidDestAddrSubunit         CommandStatus = 0x00000106
	StatusInvalidBroadcastFrequency      CommandStatus = 0x00000107
	StatusInvalidBroadcastAliasName      CommandStatus = 0x00000108
	StatusInvalidBroadcastAreaFormat     CommandStatus = 0x00000109
	StatusInvalidNumberOfBroadcastAreas  CommandStatus = 0x0000010A
	StatusInvalidBroadcastContentType    CommandStatus = 0x0000010B
	StatusInvalidBroadcastMessageClass   CommandStatus = 0x0000010C
	StatusBroadcastFailed                CommandStatus = 0x0000010D
	StatusBroadcastQueryFailed           CommandStatus = 0x0000010E
	StatusBroadcastCancelFailed          CommandStatus = 0x0000010F
	StatusInvalidBroadcastRepetition     CommandStatus = 0x00000110
	StatusInvalidBroadcastServiceGroup   CommandStatus = 0x00000111
	StatusInvalidBroadcastChannel        CommandStatus = 0x00000112
)

// OK reports whether the command status represents success.
func (s CommandStatus) OK() bool {
	return s == StatusOK
}
