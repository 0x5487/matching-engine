package protocol

// Serializer defines the contract for serializing and deserializing command payloads.
// This allows different teams to choose their preferred format (JSON, Protobuf, SBE, etc.)
// while interacting with the Matching Engine.
type Serializer interface {
	// Marshal serializes a Go struct (e.g. PlaceOrderCommand) into bytes.
	Marshal(v any) ([]byte, error)

	// Unmarshal deserializes bytes into a Go struct.
	// v must be a pointer to the target struct.
	Unmarshal(data []byte, v any) error
}
