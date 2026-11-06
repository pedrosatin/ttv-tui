package chat

import "testing"

func deletedFixture(t *testing.T) *Buffer {
	t.Helper()
	buf := NewBuffer(10, 4096)
	buf.SetGeneration(1)
	for _, m := range []Message{
		{Generation: 1, Kind: MessageChat, ID: "a1", User: "alice", DisplayName: "Alice", Body: "oi"},
		{Generation: 1, Kind: MessageChat, ID: "x1", User: "xingador", DisplayName: "Xingador", Body: "primeira ofensa"},
		{Generation: 1, Kind: MessageChat, ID: "x2", User: "xingador", DisplayName: "Xingador", Body: "segunda ofensa"},
		{Generation: 1, Kind: MessageSystem, ID: "x3", User: "xingador", DisplayName: "Xingador", MsgID: "resub", Body: "resub com ofensa"},
		{Generation: 1, Kind: MessageChat, ID: "b1", User: "bob", DisplayName: "Bob", Body: "tchau"},
	} {
		if !buf.Append(m) {
			t.Fatalf("append %q refused", m.ID)
		}
	}
	return buf
}

func deletedIDs(buf *Buffer) []string {
	var ids []string
	for _, m := range buf.Messages() {
		if m.Deleted {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

func assertSameIDs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}

func TestBuffer_ClearMsgMarksAndKeepsText(t *testing.T) {
	buf := deletedFixture(t)
	before := buf.Bytes()

	buf.ClearMsg(1, "x1")

	if buf.Len() != 5 {
		t.Fatalf("Len = %d, want 5: an erased message stays in the history", buf.Len())
	}
	assertSameIDs(t, deletedIDs(buf), []string{"x1"})
	if got := buf.Messages()[1].Body; got != "primeira ofensa" {
		t.Fatalf("Body = %q, the text is the evidence", got)
	}
	if buf.Bytes() != before {
		t.Fatalf("Bytes %d -> %d: marking must not change the accounted size", before, buf.Bytes())
	}
}

func TestBuffer_ClearChatMarksEveryMessageOfTheTarget(t *testing.T) {
	buf := deletedFixture(t)

	buf.ClearChat(1, "XINGADOR")

	if buf.Len() != 5 {
		t.Fatalf("Len = %d, want 5", buf.Len())
	}
	assertSameIDs(t, deletedIDs(buf), []string{"x1", "x2", "x3"})
	want := []string{"oi", "primeira ofensa", "segunda ofensa", "resub com ofensa", "tchau"}
	for i, m := range buf.Messages() {
		if m.Body != want[i] {
			t.Fatalf("message %d Body = %q, want %q", i, m.Body, want[i])
		}
	}
}

func TestBuffer_RepeatedClearIsIdempotent(t *testing.T) {
	buf := deletedFixture(t)
	buf.ClearMsg(1, "x1")
	buf.ClearChat(1, "xingador")
	buf.ClearChat(1, "xingador")
	buf.ClearMsg(1, "x1")
	if buf.Len() != 5 {
		t.Fatalf("Len = %d, want 5", buf.Len())
	}
	assertSameIDs(t, deletedIDs(buf), []string{"x1", "x2", "x3"})
}

func TestBuffer_ClearChatWithoutTargetStillEmpties(t *testing.T) {
	buf := deletedFixture(t)
	buf.ClearChat(1, "  ")
	if buf.Len() != 0 || buf.Bytes() != 0 {
		t.Fatalf("Len=%d Bytes=%d, want an empty buffer", buf.Len(), buf.Bytes())
	}
}

func TestBuffer_StaleGenerationMarksNothing(t *testing.T) {
	buf := deletedFixture(t)
	buf.ClearChat(9, "xingador")
	buf.ClearMsg(9, "a1")
	if ids := deletedIDs(buf); len(ids) != 0 {
		t.Fatalf("stale generation marked %v", ids)
	}
}

// The buffer keeps what moderation erased only while it would have kept any
// other message: same limits, same eviction, same accounting.
func TestBuffer_DeletedMessagesObeyTheLimits(t *testing.T) {
	t.Run("lines", func(t *testing.T) {
		buf := NewBuffer(3, 4096)
		buf.SetGeneration(1)
		for _, id := range []string{"m1", "m2", "m3"} {
			buf.Append(Message{Generation: 1, Kind: MessageChat, ID: id, User: "u", Body: "x"})
		}
		buf.ClearChat(1, "u")
		buf.Append(Message{Generation: 1, Kind: MessageChat, ID: "m4", User: "v", Body: "x"})

		assertSameIDs(t, deletedIDs(buf), []string{"m2", "m3"})
		if buf.Len() != 3 {
			t.Fatalf("Len = %d, want the 3-line limit", buf.Len())
		}
	})

	t.Run("bytes", func(t *testing.T) {
		probe := Message{Generation: 1, Kind: MessageChat, ID: "m0", User: "u", Body: "0123456789"}
		limit := probe.ByteSize() * 2
		buf := NewBuffer(100, limit)
		buf.SetGeneration(1)
		for _, id := range []string{"m1", "m2"} {
			m := probe
			m.ID = id
			buf.Append(m)
		}
		buf.ClearChat(1, "u")
		m := probe
		m.ID = "m3"
		m.User = "v"
		buf.Append(m)

		assertSameIDs(t, deletedIDs(buf), []string{"m2"})
		sum := 0
		for _, item := range buf.Messages() {
			sum += item.ByteSize()
		}
		if buf.Bytes() != sum || buf.Bytes() > limit {
			t.Fatalf("Bytes = %d, sum = %d, limit = %d", buf.Bytes(), sum, limit)
		}
	})
}
