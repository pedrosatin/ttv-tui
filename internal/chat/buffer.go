package chat

import (
	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// Default buffer capacity limits:
const (
	DefaultMaxLines = catalog.DefaultChatMaxLines
	DefaultMaxBytes = catalog.DefaultChatMaxBytes
)

// MessageKind classifies an entry stored in the chat buffer.
type MessageKind = catalog.ChatMessageKind

const (
	MessageChat   = catalog.ChatMessageChat
	MessageNotice = catalog.ChatMessageNotice
	MessageSystem = catalog.ChatMessageSystem
)

// Message is a single displayable item in the chat buffer.
type Message = catalog.ChatMessage

// Buffer is a circular message buffer bounded simultaneously by line count and total byte footprint.
type Buffer = catalog.ChatBuffer

// NewBuffer creates a Buffer bounded by maxLines and maxBytes.
func NewBuffer(maxLines, maxBytes int) *Buffer {
	return catalog.NewChatBuffer(maxLines, maxBytes)
}
