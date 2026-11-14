package youtube

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

func TestClient_PollChatWithRealFixture(t *testing.T) {
	fixture := loadFixture(t, "get_live_chat.json")

	var gotPath string
	var gotPayload innertubeChatPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, HTTPClient: srv.Client()})
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	res, err := client.PollChat(context.Background(), PollRequest{
		APIKey:        "PUBLIC_TEST_API_KEY",
		ClientVersion: "2.20261112.10.00",
		Continuation:  "TEST_RELOAD_CONTINUATION",
	}, now)
	if err != nil {
		t.Fatalf("PollChat: %v", err)
	}

	if gotPath != "/youtubei/v1/live_chat/get_live_chat?key=PUBLIC_TEST_API_KEY" {
		t.Errorf("request path = %q", gotPath)
	}
	if gotPayload.Context.Client.ClientName != "WEB" || gotPayload.Context.Client.ClientVersion != "2.20261112.10.00" || gotPayload.Continuation != "TEST_RELOAD_CONTINUATION" {
		t.Errorf("payload = %+v", gotPayload)
	}
	if res.NextContinuation != "TEST_NEXT_CONTINUATION" {
		t.Errorf("NextContinuation = %q, want TEST_NEXT_CONTINUATION", res.NextContinuation)
	}
	if res.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", res.Timeout)
	}
	if !slices.Equal(res.DeletedIDs, []string{"ChwKGkNNT3RoOGJvaDVnREZSQWYxZ0FkQ3FZZEZB"}) {
		t.Errorf("DeletedIDs = %v", res.DeletedIDs)
	}

	// Real fixture has 1 giftMessageViewModel + 6 liveChatTextMessageRenderer;
	// liveChatViewerEngagementMessageRenderer is skipped.
	if len(res.Messages) != 7 {
		t.Fatalf("len(Messages) = %d, want 7", len(res.Messages))
	}
	if res.Messages[0].Kind != catalog.ChatMessageNotice || res.Messages[0].DisplayName != "@toaster_scramble" || res.Messages[0].Body != "sent Star" {
		t.Errorf("Messages[0] = %+v", res.Messages[0])
	}
	if res.Messages[1].DisplayName != "@Luffy-j7u2n" || res.Messages[1].Body != "kya howw??" {
		t.Errorf("Messages[1] = %+v", res.Messages[1])
	}
	last := res.Messages[len(res.Messages)-1]
	if last.DisplayName != "@Mortalmp4" || last.Body != "udhar Reze baat kr rha hai 🥀🥀" {
		t.Errorf("last message = %+v", last)
	}
}

func TestParseLiveChatResponse_AdditionalRenderers(t *testing.T) {
	raw := []byte(`{
	  "continuationContents": {
	    "liveChatContinuation": {
	      "continuations": [
	        {"timedContinuationData": {"timeoutMs": 5500, "continuation": "NEXT_TIMED"}}
	      ],
	      "actions": [
	        {"addChatItemAction": {"item": {"liveChatPaidMessageRenderer": {
	          "id": "sc1",
	          "timestampUsec": "1700000000000000",
	          "authorName": {"simpleText": "@Sponsor"},
	          "authorBadges": [{"liveChatAuthorBadgeRenderer": {"icon": {"iconType": "MODERATOR"}}}],
	          "purchaseAmountText": {"simpleText": "$5.00"},
	          "message": {"runs": [{"text": "hello "}, {"emoji": {"emojiId": "custom_id", "isCustomEmoji": true, "shortcuts": [":wave:"]}}]}
	        }}}},
	        {"addChatItemAction": {"item": {"liveChatMembershipItemRenderer": {
	          "id": "mem1",
	          "authorName": {"simpleText": "@Member"},
	          "headerSubtext": {"runs": [{"text": "Welcome to membership"}]}
	        }}}},
	        {"addChatItemAction": {"item": {"liveChatSponsorshipsGiftPurchaseAnnouncementRenderer": {
	          "id": "gift1",
	          "header": {"liveChatSponsorshipsHeaderRenderer": {
	            "authorName": {"simpleText": "@Gifter"},
	            "primaryText": {"runs": [{"text": "Gifted 5 memberships"}]}
	          }}
	        }}}},
	        {"markChatItemAsDeletedAction": {"targetItemId": "sc1"}}
	      ]
	    }
	  }
	}`)

	now := time.Unix(1700000001, 0)
	res, err := parseLiveChatResponse(raw, now)
	if err != nil {
		t.Fatalf("parseLiveChatResponse: %v", err)
	}
	if res.NextContinuation != "NEXT_TIMED" || res.Timeout != 5500*time.Millisecond {
		t.Errorf("continuation = %q timeout = %v", res.NextContinuation, res.Timeout)
	}
	if !slices.Equal(res.DeletedIDs, []string{"sc1"}) {
		t.Errorf("DeletedIDs = %v", res.DeletedIDs)
	}
	if len(res.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(res.Messages))
	}
	sc := res.Messages[0]
	if !sc.Highlighted || !slices.Equal(sc.Badges, []string{"moderator"}) || sc.Body != "$5.00 — hello :wave:" {
		t.Errorf("superchat = %+v", sc)
	}
	if res.Messages[1].Kind != catalog.ChatMessageNotice || res.Messages[1].Body != "Welcome to membership" {
		t.Errorf("membership = %+v", res.Messages[1])
	}
	if res.Messages[2].Kind != catalog.ChatMessageNotice || res.Messages[2].Body != "Gifted 5 memberships" {
		t.Errorf("gift = %+v", res.Messages[2])
	}
}
