package app

import (
	"context"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/provider"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// newTwitchChatBridge returns a bridge whose only chat is mgr, registered the
// way Run registers the Twitch one.
func newTwitchChatBridge(t *testing.T, mgr *chat.Manager) *chatBridge {
	t.Helper()
	reg := provider.NewRegistry(twitch.NewProvider(nil, twitch.WithChat(mgr)))
	return newChatBridge(context.Background(), reg)
}
