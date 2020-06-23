package telepathy

const (
	MessageEventType_Connected    byte = 0
	MessageEventType_Data         byte = 1
	MessageEventType_Disconnected byte = 2
)

const (
	MAX_MESSAGE_SIZE int = 16 * 1024
)

type Message struct {
	EventType    byte
	ConnectionId int
	Data         []byte
}
