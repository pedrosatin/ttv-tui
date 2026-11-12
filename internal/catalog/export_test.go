package catalog

// SeenLen returns the number of remembered message IDs.
func (b *ChatBuffer) SeenLen() int { return len(b.seen) }

// SeenRingLen returns the capacity/size of the remembered ID ring buffer.
func (b *ChatBuffer) SeenRingLen() int { return len(b.seenRing) }
