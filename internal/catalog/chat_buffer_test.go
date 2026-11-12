package catalog

import (
	"fmt"
	"testing"
)

func TestChatBuffer_RememberedIDsStayBounded(t *testing.T) {
	const limit = 3
	b := NewChatBuffer(limit, 4096)
	b.SetGeneration(1)
	msg := func(id string) ChatMessage {
		return ChatMessage{Generation: 1, Kind: ChatMessageChat, ID: id, User: "u", DisplayName: "U", Body: "body " + id}
	}
	for i := range 100 {
		b.Append(msg(fmt.Sprintf("m%d", i)))
	}
	if b.SeenLen() != 2*limit || b.SeenRingLen() != 2*limit {
		t.Fatalf("seen=%d ring=%d, want %d", b.SeenLen(), b.SeenRingLen(), 2*limit)
	}
	if !b.Append(msg("m0")) {
		t.Fatal("an ID past the memory must be accepted")
	}
}
