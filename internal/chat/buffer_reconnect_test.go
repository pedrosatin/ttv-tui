package chat

import (
	"fmt"
	"testing"
)

func bufferMsg(gen uint64, id string) Message {
	return Message{Generation: gen, Kind: MessageChat, ID: id, User: "u", DisplayName: "U", Body: "body " + id}
}

func TestBuffer_AdoptGenerationKeepsMessages(t *testing.T) {
	b := NewBuffer(10, 4096)
	b.SetGeneration(1)
	b.Append(bufferMsg(1, "a"))
	bytes := b.Bytes()

	b.AdoptGeneration(2)

	if b.Len() != 1 || b.Bytes() != bytes {
		t.Fatalf("Len=%d Bytes=%d, want the message kept (%d bytes)", b.Len(), b.Bytes(), bytes)
	}
	if b.Append(bufferMsg(1, "late")) {
		t.Fatal("message of the old session accepted after the adoption")
	}
	if !b.Append(bufferMsg(2, "b")) {
		t.Fatal("message of the new session refused")
	}
	// Moderation of the new session still reaches messages of the old one.
	b.ClearMsg(2, "a")
	if !b.Messages()[0].Deleted {
		t.Fatal("CLEARMSG of the new session missed a preserved message")
	}
}

func TestBuffer_SetGenerationStillClears(t *testing.T) {
	b := NewBuffer(10, 4096)
	b.SetGeneration(1)
	b.Append(bufferMsg(1, "a"))

	b.SetGeneration(2)

	if b.Len() != 0 {
		t.Fatalf("Len=%d, want a channel change to clear", b.Len())
	}
	if !b.Append(bufferMsg(2, "a")) {
		t.Fatal("IDs of the previous channel must not count as replays")
	}
}

func TestBuffer_DropsReplayedIDs(t *testing.T) {
	b := NewBuffer(10, 4096)
	b.SetGeneration(1)
	if !b.Append(bufferMsg(1, "a")) {
		t.Fatal("first delivery refused")
	}
	if b.Append(bufferMsg(1, "a")) {
		t.Fatal("replay accepted")
	}
	if b.Len() != 1 {
		t.Fatalf("Len=%d, want 1", b.Len())
	}
}

// Notices and local lines have no ID, and equal text is not a replay.
func TestBuffer_MessagesWithoutIDAreNeverReplays(t *testing.T) {
	b := NewBuffer(10, 4096)
	b.SetGeneration(1)
	line := Message{Generation: 1, Kind: MessageNotice, Body: "same"}
	for range 2 {
		if !b.Append(line) {
			t.Fatal("message without ID dropped")
		}
	}
}

func TestBuffer_ReplayOfEvictedMessagesDoesNotRotateHistory(t *testing.T) {
	const limit = 5
	b := NewBuffer(limit, 4096)
	b.SetGeneration(1)
	for i := range limit + 2 {
		b.Append(bufferMsg(1, fmt.Sprintf("m%d", i)))
	}
	want := b.Messages()

	for i := range limit + 2 {
		if b.Append(bufferMsg(1, fmt.Sprintf("m%d", i))) {
			t.Fatalf("replay of m%d accepted", i)
		}
	}

	got := b.Messages()
	for i := range want {
		if got[i].ID != want[i].ID {
			t.Fatalf("history changed at %d: %s, want %s", i, got[i].ID, want[i].ID)
		}
	}
}

func TestBuffer_RememberedIDsStayBounded(t *testing.T) {
	const limit = 3
	b := NewBuffer(limit, 4096)
	b.SetGeneration(1)
	for i := range 100 {
		b.Append(bufferMsg(1, fmt.Sprintf("m%d", i)))
	}
	if b.SeenLen() != 2*limit || b.SeenRingLen() != 2*limit {
		t.Fatalf("seen=%d ring=%d, want %d", b.SeenLen(), b.SeenRingLen(), 2*limit)
	}
	if !b.Append(bufferMsg(1, "m0")) {
		t.Fatal("an ID past the memory must be accepted")
	}
}
