package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// PollRequest identifies a live-chat continuation poll to YouTube innertube.
type PollRequest struct {
	APIKey        string
	ClientVersion string
	Continuation  string
}

// PollResult holds the messages, deletions, and next polling directive returned
// by one get_live_chat response.
type PollResult struct {
	NextContinuation string
	Timeout          time.Duration
	Messages         []catalog.ChatMessage
	DeletedIDs       []string
}

type innertubeChatPayload struct {
	Context struct {
		Client struct {
			ClientName    string `json:"clientName"`
			ClientVersion string `json:"clientVersion"`
		} `json:"client"`
	} `json:"context"`
	Continuation string `json:"continuation"`
}

// PollChat posts one unauthenticated innertube get_live_chat poll and decodes
// its actions and next continuation token.
func (c *Client) PollChat(ctx context.Context, req PollRequest, now time.Time) (PollResult, error) {
	const op = "poll_chat"
	cont := strings.TrimSpace(req.Continuation)
	if cont == "" {
		return PollResult{}, contractError(op, errors.New("missing continuation"))
	}
	clientVer := strings.TrimSpace(req.ClientVersion)
	if clientVer == "" {
		clientVer = defaultClientVersion
	}

	var payload innertubeChatPayload
	payload.Context.Client.ClientName = "WEB"
	payload.Context.Client.ClientVersion = clientVer
	payload.Continuation = cont

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return PollResult{}, contractError(op, fmt.Errorf("encode request: %w", err))
	}

	endpoint := c.baseURL + "/youtubei/v1/live_chat/get_live_chat"
	if key := strings.TrimSpace(req.APIKey); key != "" {
		endpoint += "?key=" + url.QueryEscape(key)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return PollResult{}, contractError(op, fmt.Errorf("build request: %w", err))
	}
	httpReq.Header.Set("User-Agent", browserUserAgent)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Origin", DefaultBaseURL)

	res, err := c.http.Do(httpReq)
	if err != nil {
		return PollResult{}, &catalog.Error{Platform: platform.YouTube, Kind: catalog.Rede, Op: op, Err: err}
	}
	defer func() { _ = res.Body.Close() }()

	respBytes, readErr := io.ReadAll(io.LimitReader(res.Body, maxJSONBytes))
	if res.StatusCode != http.StatusOK {
		return PollResult{}, statusError(op, res.StatusCode, ErrChatDisabled)
	}
	if readErr != nil {
		return PollResult{}, &catalog.Error{Platform: platform.YouTube, Kind: catalog.Rede, Op: op, Err: fmt.Errorf("read response: %w", readErr)}
	}
	if now.IsZero() {
		now = time.Now()
	}
	return parseLiveChatResponse(respBytes, now)
}

type innertubeText struct {
	SimpleText string         `json:"simpleText"`
	Content    string         `json:"content"`
	Runs       []innertubeRun `json:"runs"`
}

type innertubeRun struct {
	Text  string `json:"text"`
	Emoji *struct {
		EmojiID       string   `json:"emojiId"`
		Shortcuts     []string `json:"shortcuts"`
		IsCustomEmoji bool     `json:"isCustomEmoji"`
		Accessibility *struct {
			AccessibilityData *struct {
				Label string `json:"label"`
			} `json:"accessibilityData"`
		} `json:"accessibility"`
	} `json:"emoji"`
}

type innertubeBadge struct {
	LiveChatAuthorBadgeRenderer *struct {
		Icon *struct {
			IconType string `json:"iconType"`
		} `json:"icon"`
	} `json:"liveChatAuthorBadgeRenderer"`
}

type innertubeChatItem struct {
	LiveChatTextMessageRenderer *struct {
		ID            string           `json:"id"`
		TimestampUsec string           `json:"timestampUsec"`
		AuthorName    *innertubeText   `json:"authorName"`
		AuthorBadges  []innertubeBadge `json:"authorBadges"`
		Message       *innertubeText   `json:"message"`
	} `json:"liveChatTextMessageRenderer"`
	LiveChatPaidMessageRenderer *struct {
		ID                 string           `json:"id"`
		TimestampUsec      string           `json:"timestampUsec"`
		AuthorName         *innertubeText   `json:"authorName"`
		AuthorBadges       []innertubeBadge `json:"authorBadges"`
		PurchaseAmountText *innertubeText   `json:"purchaseAmountText"`
		Message            *innertubeText   `json:"message"`
	} `json:"liveChatPaidMessageRenderer"`
	LiveChatPaidStickerRenderer *struct {
		ID                 string           `json:"id"`
		TimestampUsec      string           `json:"timestampUsec"`
		AuthorName         *innertubeText   `json:"authorName"`
		AuthorBadges       []innertubeBadge `json:"authorBadges"`
		PurchaseAmountText *innertubeText   `json:"purchaseAmountText"`
		Sticker            *struct {
			Accessibility *struct {
				AccessibilityData *struct {
					Label string `json:"label"`
				} `json:"accessibilityData"`
			} `json:"accessibility"`
		} `json:"sticker"`
	} `json:"liveChatPaidStickerRenderer"`
	LiveChatMembershipItemRenderer *struct {
		ID                string           `json:"id"`
		TimestampUsec     string           `json:"timestampUsec"`
		AuthorName        *innertubeText   `json:"authorName"`
		AuthorBadges      []innertubeBadge `json:"authorBadges"`
		HeaderPrimaryText *innertubeText   `json:"headerPrimaryText"`
		HeaderSubtext     *innertubeText   `json:"headerSubtext"`
		Message           *innertubeText   `json:"message"`
	} `json:"liveChatMembershipItemRenderer"`
	LiveChatSponsorshipsGiftPurchaseAnnouncementRenderer *struct {
		ID            string `json:"id"`
		TimestampUsec string `json:"timestampUsec"`
		Header        *struct {
			LiveChatSponsorshipsHeaderRenderer *struct {
				AuthorName   *innertubeText   `json:"authorName"`
				AuthorBadges []innertubeBadge `json:"authorBadges"`
				PrimaryText  *innertubeText   `json:"primaryText"`
			} `json:"liveChatSponsorshipsHeaderRenderer"`
		} `json:"header"`
	} `json:"liveChatSponsorshipsGiftPurchaseAnnouncementRenderer"`
	GiftMessageViewModel *struct {
		ID         string         `json:"id"`
		AuthorName *innertubeText `json:"authorName"`
		Text       *innertubeText `json:"text"`
	} `json:"giftMessageViewModel"`
	// Boilerplate guidelines notice ignored by design:
	// liveChatViewerEngagementMessageRenderer.
}

type innertubeContinuationData struct {
	Continuation string `json:"continuation"`
	TimeoutMs    int    `json:"timeoutMs"`
}

type innertubeChatResponse struct {
	ContinuationContents *struct {
		LiveChatContinuation *struct {
			Continuations []struct {
				InvalidationContinuationData   *innertubeContinuationData `json:"invalidationContinuationData"`
				TimedContinuationData          *innertubeContinuationData `json:"timedContinuationData"`
				LiveChatReplayContinuationData *innertubeContinuationData `json:"liveChatReplayContinuationData"`
			} `json:"continuations"`
			Actions []struct {
				AddChatItemAction *struct {
					Item *innertubeChatItem `json:"item"`
				} `json:"addChatItemAction"`
				RemoveChatItemAction *struct {
					TargetItemID string `json:"targetItemId"`
				} `json:"removeChatItemAction"`
				MarkChatItemAsDeletedAction *struct {
					TargetItemID string `json:"targetItemId"`
				} `json:"markChatItemAsDeletedAction"`
			} `json:"actions"`
		} `json:"liveChatContinuation"`
	} `json:"continuationContents"`
}

// parseLiveChatResponse decodes a YouTube innertube get_live_chat JSON body.
func parseLiveChatResponse(raw []byte, now time.Time) (PollResult, error) {
	const op = "poll_chat"
	var resp innertubeChatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return PollResult{}, contractError(op, fmt.Errorf("decode live chat response: %w", err))
	}
	if resp.ContinuationContents == nil || resp.ContinuationContents.LiveChatContinuation == nil {
		return PollResult{}, contractError(op, errors.New("live chat response missing continuationContents"))
	}
	lc := resp.ContinuationContents.LiveChatContinuation

	var out PollResult
	for _, cont := range lc.Continuations {
		for _, candidate := range []*innertubeContinuationData{
			cont.InvalidationContinuationData,
			cont.TimedContinuationData,
			cont.LiveChatReplayContinuationData,
		} {
			if candidate != nil && strings.TrimSpace(candidate.Continuation) != "" {
				out.NextContinuation = strings.TrimSpace(candidate.Continuation)
				if candidate.TimeoutMs > 0 {
					out.Timeout = time.Duration(candidate.TimeoutMs) * time.Millisecond
				}
				break
			}
		}
		if out.NextContinuation != "" {
			break
		}
	}

	for _, act := range lc.Actions {
		if act.RemoveChatItemAction != nil {
			if id := strings.TrimSpace(act.RemoveChatItemAction.TargetItemID); id != "" {
				out.DeletedIDs = append(out.DeletedIDs, id)
			}
		}
		if act.MarkChatItemAsDeletedAction != nil {
			if id := strings.TrimSpace(act.MarkChatItemAsDeletedAction.TargetItemID); id != "" {
				out.DeletedIDs = append(out.DeletedIDs, id)
			}
		}
		if act.AddChatItemAction == nil || act.AddChatItemAction.Item == nil {
			continue
		}
		if msg, ok := parseChatItem(act.AddChatItemAction.Item, now); ok {
			out.Messages = append(out.Messages, msg)
		}
	}

	return out, nil
}

func parseChatItem(item *innertubeChatItem, now time.Time) (catalog.ChatMessage, bool) {
	switch {
	case item.LiveChatTextMessageRenderer != nil:
		r := item.LiveChatTextMessageRenderer
		author := extractText(r.AuthorName)
		body := extractText(r.Message)
		if author == "" || body == "" {
			return catalog.ChatMessage{}, false
		}
		return catalog.ChatMessage{
			ID:          strings.TrimSpace(r.ID),
			Kind:        catalog.ChatMessageChat,
			ReceivedAt:  parseTimestampUsec(r.TimestampUsec, now),
			User:        author,
			DisplayName: author,
			Badges:      extractBadges(r.AuthorBadges),
			Body:        body,
		}, true

	case item.LiveChatPaidMessageRenderer != nil:
		r := item.LiveChatPaidMessageRenderer
		author := extractText(r.AuthorName)
		amount := extractText(r.PurchaseAmountText)
		msgText := extractText(r.Message)
		body := joinNonEmpty(" — ", amount, msgText)
		if author == "" || body == "" {
			return catalog.ChatMessage{}, false
		}
		return catalog.ChatMessage{
			ID:          strings.TrimSpace(r.ID),
			Kind:        catalog.ChatMessageChat,
			ReceivedAt:  parseTimestampUsec(r.TimestampUsec, now),
			User:        author,
			DisplayName: author,
			Badges:      extractBadges(r.AuthorBadges),
			Highlighted: true,
			Body:        body,
		}, true

	case item.LiveChatPaidStickerRenderer != nil:
		r := item.LiveChatPaidStickerRenderer
		author := extractText(r.AuthorName)
		amount := extractText(r.PurchaseAmountText)
		sticker := ""
		if r.Sticker != nil && r.Sticker.Accessibility != nil && r.Sticker.Accessibility.AccessibilityData != nil {
			sticker = strings.TrimSpace(r.Sticker.Accessibility.AccessibilityData.Label)
		}
		body := joinNonEmpty(" ", amount, sticker)
		if author == "" || body == "" {
			return catalog.ChatMessage{}, false
		}
		return catalog.ChatMessage{
			ID:          strings.TrimSpace(r.ID),
			Kind:        catalog.ChatMessageChat,
			ReceivedAt:  parseTimestampUsec(r.TimestampUsec, now),
			User:        author,
			DisplayName: author,
			Badges:      extractBadges(r.AuthorBadges),
			Highlighted: true,
			Body:        body,
		}, true

	case item.LiveChatMembershipItemRenderer != nil:
		r := item.LiveChatMembershipItemRenderer
		author := extractText(r.AuthorName)
		header := joinNonEmpty(" ", extractText(r.HeaderPrimaryText), extractText(r.HeaderSubtext))
		msgText := extractText(r.Message)
		body := joinNonEmpty(" — ", header, msgText)
		if body == "" {
			return catalog.ChatMessage{}, false
		}
		return catalog.ChatMessage{
			ID:          strings.TrimSpace(r.ID),
			Kind:        catalog.ChatMessageNotice,
			ReceivedAt:  parseTimestampUsec(r.TimestampUsec, now),
			User:        author,
			DisplayName: author,
			Badges:      extractBadges(r.AuthorBadges),
			Highlighted: true,
			Body:        body,
		}, true

	case item.LiveChatSponsorshipsGiftPurchaseAnnouncementRenderer != nil:
		r := item.LiveChatSponsorshipsGiftPurchaseAnnouncementRenderer
		if r.Header == nil || r.Header.LiveChatSponsorshipsHeaderRenderer == nil {
			return catalog.ChatMessage{}, false
		}
		h := r.Header.LiveChatSponsorshipsHeaderRenderer
		author := extractText(h.AuthorName)
		body := extractText(h.PrimaryText)
		if author == "" && body == "" {
			return catalog.ChatMessage{}, false
		}
		return catalog.ChatMessage{
			ID:          strings.TrimSpace(r.ID),
			Kind:        catalog.ChatMessageNotice,
			ReceivedAt:  parseTimestampUsec(r.TimestampUsec, now),
			User:        author,
			DisplayName: author,
			Badges:      extractBadges(h.AuthorBadges),
			Highlighted: true,
			Body:        body,
		}, true

	case item.GiftMessageViewModel != nil:
		g := item.GiftMessageViewModel
		author := extractText(g.AuthorName)
		body := extractText(g.Text)
		if author == "" || body == "" {
			return catalog.ChatMessage{}, false
		}
		return catalog.ChatMessage{
			ID:          strings.TrimSpace(g.ID),
			Kind:        catalog.ChatMessageNotice,
			ReceivedAt:  now,
			User:        author,
			DisplayName: author,
			Body:        body,
		}, true

	default:
		return catalog.ChatMessage{}, false
	}
}

func extractText(t *innertubeText) string {
	if t == nil {
		return ""
	}
	if len(t.Runs) > 0 {
		var b strings.Builder
		for _, r := range t.Runs {
			if r.Text != "" {
				b.WriteString(r.Text)
				continue
			}
			if r.Emoji != nil {
				switch {
				case r.Emoji.EmojiID != "" && !r.Emoji.IsCustomEmoji:
					b.WriteString(r.Emoji.EmojiID)
				case len(r.Emoji.Shortcuts) > 0 && r.Emoji.Shortcuts[0] != "":
					b.WriteString(r.Emoji.Shortcuts[0])
				case r.Emoji.Accessibility != nil && r.Emoji.Accessibility.AccessibilityData != nil:
					b.WriteString(r.Emoji.Accessibility.AccessibilityData.Label)
				case r.Emoji.EmojiID != "":
					b.WriteString(r.Emoji.EmojiID)
				}
			}
		}
		if s := strings.TrimSpace(b.String()); s != "" {
			return s
		}
	}
	if s := strings.TrimSpace(t.SimpleText); s != "" {
		return s
	}
	return strings.TrimSpace(t.Content)
}

func extractBadges(raw []innertubeBadge) []string {
	var badges []string
	for _, b := range raw {
		if b.LiveChatAuthorBadgeRenderer == nil || b.LiveChatAuthorBadgeRenderer.Icon == nil {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(b.LiveChatAuthorBadgeRenderer.Icon.IconType)) {
		case "OWNER":
			badges = append(badges, "broadcaster")
		case "MODERATOR":
			badges = append(badges, "moderator")
		}
	}
	return badges
}

func parseTimestampUsec(raw string, fallback time.Time) time.Time {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback
	}
	usec, err := strconv.ParseInt(s, 10, 64)
	if err != nil || usec <= 0 {
		return fallback
	}
	return time.UnixMicro(usec)
}

func joinNonEmpty(sep string, parts ...string) string {
	var clean []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			clean = append(clean, s)
		}
	}
	return strings.Join(clean, sep)
}
