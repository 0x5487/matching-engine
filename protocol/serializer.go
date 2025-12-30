package protocol

import "github.com/bytedance/sonic"

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

// DefaultJSONSerializer is a high-performance JSON serializer using the sonic library.
type DefaultJSONSerializer struct{}

// Marshal implements the Serializer interface.
func (s *DefaultJSONSerializer) Marshal(v any) ([]byte, error) {
	return sonic.Marshal(v)
}

// Unmarshal implements the Serializer interface.
func (s *DefaultJSONSerializer) Unmarshal(data []byte, v any) error {
	return sonic.Unmarshal(data, v)
}
