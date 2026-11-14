// Package youtube reads YouTube live stream state and read-only chat without an
// account or API quota: watch-page metadata comes from the HTML page the site
// serves, and live chat comes from innertube polling without cookies.
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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/streamurl"
)

const (
	// DefaultBaseURL is the YouTube web origin.
	DefaultBaseURL = "https://www.youtube.com"
	// DefaultRequestTimeout bounds a single watch-page or innertube request.
	DefaultRequestTimeout = 15 * time.Second
	// defaultClientVersion is used when a watch page omits INNERTUBE_CLIENT_VERSION.
	defaultClientVersion = "2.20261112.10.00"
	// maxHTMLBytes bounds a watch page HTML response (observed live pages are ~1.0-1.7 MiB).
	maxHTMLBytes = 8 << 20
	// maxJSONBytes bounds an innertube JSON response.
	maxJSONBytes = 2 << 20
	// resolveConcurrency bounds parallel watch-page lookups in ChannelsByIDs.
	resolveConcurrency = 4
	browserUserAgent   = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	// ErrInvalidTarget reports a target that does not describe a valid YouTube video or @handle.
	ErrInvalidTarget = errors.New("youtube: invalid channel or video target")
	// ErrChannelNotFound reports a channel or video that YouTube answered with 404 or unavailable.
	ErrChannelNotFound = errors.New("youtube: channel or video not found")
	// ErrOffline reports a channel without an active live broadcast or an upcoming offline slate.
	ErrOffline = errors.New("youtube: channel has no live stream")
	// ErrNotLive reports a video that is on demand (VOD) rather than an active live stream.
	ErrNotLive = errors.New("youtube: video is not live")
	// ErrBotCheck reports an anti-bot verification challenge ("Sign in to confirm you're not a bot").
	ErrBotCheck = errors.New("youtube: bot verification requested")
	// ErrChatDisabled reports an active live stream that has no live chat continuation.
	ErrChatDisabled = errors.New("youtube: live chat is unavailable for this stream")
)

// ClientConfig configures a Client. Zero values select production defaults.
type ClientConfig struct {
	BaseURL    string
	HTTPClient *http.Client
}

// Client resolves YouTube live streams and polls innertube live chat.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a Client configured by cfg.
func NewClient(cfg ClientConfig) *Client {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: DefaultRequestTimeout}
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	return &Client{
		baseURL: base,
		http:    hc,
	}
}

// TargetKind distinguishes a channel target (@handle, channel/..., c/..., user/...)
// from a video ID.
type TargetKind int

const (
	targetVideo TargetKind = iota + 1
	targetHandle
)

// Target is a validated YouTube live target (video ID or channel path).
type Target struct {
	Kind        TargetKind
	ID          string // "@Handle", "channel/<id>", "c/<name>", "user/<name>", or video ID (case-sensitive)
	FetchPath   string // path relative to baseURL, e.g. "/@Handle/live", "/channel/UC.../live", or "/watch?v=id"
	PlaybackURL string // canonical URL handed to streamlink
}

// ParseTarget parses raw (a URL, bare @handle, channel path, or stable ID) into a validated Target.
func ParseTarget(raw string) (Target, error) {
	s := strings.TrimSpace(raw)
	if s == "" || strings.ContainsAny(s, " \t\r\n\v\f") || platform.HasControl(s) {
		return Target{}, ErrInvalidTarget
	}

	if strings.Contains(s, "://") {
		info, ok := streamurl.Parse(s)
		if !ok || info.Platform != platform.YouTube {
			return Target{}, ErrInvalidTarget
		}
		if isYouTubeChannelID(info.Login) {
			// Always give streamlink and the watch-page fetch the .../live route:
			// streamlink accepts bare /@handle, /channel/..., /c/... and /user/...,
			// but picks the first videoRenderer on the channel page, which may be a VOD.
			return channelTarget(info.Login), nil
		}
		return videoTarget(info.Login, s), nil
	}

	// Strip optional "youtube:" qualifier when coming from a stored StableID/PlayerLabel.
	if strings.HasPrefix(strings.ToLower(s), platform.YouTube+":") {
		s = s[len(platform.YouTube)+1:]
		if s == "" {
			return Target{}, ErrInvalidTarget
		}
	}

	if strings.HasPrefix(s, "@") {
		if !platform.ValidYouTubeHandle(s) {
			return Target{}, ErrInvalidTarget
		}
		return channelTarget(s), nil
	}

	if kind, seg, ok := splitYouTubeChannelRef(s); ok {
		return channelTarget(kind + "/" + seg), nil
	}

	if !platform.ValidYouTubeVideoID(s) {
		return Target{}, ErrInvalidTarget
	}
	return videoTarget(s, DefaultBaseURL+"/watch?v="+s), nil
}

func isYouTubeChannelID(id string) bool {
	if strings.HasPrefix(id, "@") {
		return true
	}
	_, _, ok := splitYouTubeChannelRef(id)
	return ok
}

func splitYouTubeChannelRef(s string) (kind, seg string, ok bool) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return "", "", false
	}
	kind = strings.ToLower(parts[0])
	switch kind {
	case "channel", "c", "user":
	default:
		return "", "", false
	}
	if !platform.ValidYouTubeChannelSegment(parts[1]) {
		return "", "", false
	}
	return kind, parts[1], true
}

func channelTarget(id string) Target {
	path := "/" + id + "/live"
	return Target{
		Kind:        targetHandle,
		ID:          id,
		FetchPath:   path,
		PlaybackURL: DefaultBaseURL + path,
	}
}

func videoTarget(id, playbackURL string) Target {
	return Target{
		Kind:        targetVideo,
		ID:          id,
		FetchPath:   "/watch?v=" + url.QueryEscape(id),
		PlaybackURL: playbackURL,
	}
}

// Resolved describes a live YouTube stream found by inspecting a watch page.
type Resolved struct {
	Target           Target
	VideoID          string
	ChannelID        string
	Author           string
	Title            string
	Viewers          *int
	APIKey           string
	ClientVersion    string
	ChatContinuation string
	Availability     catalog.Availability
}

// Resolve fetches the watch page for rawTarget and classifies whether it is an
// active live stream, an offline channel, a VOD, or an anti-bot verification challenge.
func (c *Client) Resolve(ctx context.Context, rawTarget string) (Resolved, error) {
	const op = "resolve"
	target, err := ParseTarget(rawTarget)
	if err != nil {
		return Resolved{}, err
	}
	body, err := c.getHTML(ctx, op, target.FetchPath)
	if err != nil {
		return Resolved{Target: target}, err
	}
	return parseWatchPage(target, body)
}

func (c *Client) getHTML(ctx context.Context, op, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, contractError(op, fmt.Errorf("build request: %w", err))
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	res, err := c.http.Do(req)
	if err != nil {
		return nil, &catalog.Error{Platform: platform.YouTube, Kind: catalog.Rede, Op: op, Err: err}
	}
	defer func() { _ = res.Body.Close() }()

	body, readErr := io.ReadAll(io.LimitReader(res.Body, maxHTMLBytes))
	if res.StatusCode != http.StatusOK {
		return nil, statusError(op, res.StatusCode, res.Header, time.Now(), ErrChannelNotFound)
	}
	if readErr != nil {
		return nil, &catalog.Error{Platform: platform.YouTube, Kind: catalog.Rede, Op: op, Err: fmt.Errorf("read response: %w", readErr)}
	}
	return body, nil
}

// ChannelsByIDs implements catalog.Resolver for saved and pasted YouTube
// targets. Unknown/invalid targets and contract errors are omitted; offline
// channels and VODs map to AvailabilityOffline; blocks/rate-limits/network
// errors fail the batch like Kick's Resolver.
func (c *Client) ChannelsByIDs(ctx context.Context, ids []string) ([]catalog.Channel, error) {
	clean := uniqueTargets(ids)
	if len(clean) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]*catalog.Channel, len(clean))
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	sem := make(chan struct{}, resolveConcurrency)
	for i, id := range clean {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			ch, err := c.resolveChannel(ctx, id)
			switch {
			case err == nil:
				results[i] = &ch
			case errors.Is(err, ErrChannelNotFound), errors.Is(err, ErrInvalidTarget), catalog.KindOf(err) == catalog.Contrato:
				// No verifiable state for this entry; keep checking the others.
			default:
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	out := make([]catalog.Channel, 0, len(clean))
	for _, ch := range results {
		if ch != nil {
			out = append(out, *ch)
		}
	}
	return out, nil
}

func (c *Client) resolveChannel(ctx context.Context, id string) (catalog.Channel, error) {
	target, err := ParseTarget(id)
	if err != nil {
		return catalog.Channel{}, err
	}
	res, err := c.Resolve(ctx, id)
	stable := target.ID
	ch := catalog.Channel{
		ID:          platform.YouTube + ":" + stable,
		Login:       stable,
		DisplayName: stable,
		Platform:    platform.YouTube,
		URL:         target.PlaybackURL,
	}
	switch {
	case err == nil:
		ch.Availability = catalog.AvailabilityLive
		ch.Viewers = res.Viewers
		if name := strings.TrimSpace(res.Author); name != "" {
			ch.DisplayName = name
		}
		if title := strings.TrimSpace(res.Title); title != "" {
			ch.Title = &title
		}
		return ch, nil
	case errors.Is(err, ErrOffline), errors.Is(err, ErrNotLive):
		ch.Availability = catalog.AvailabilityOffline
		if name := strings.TrimSpace(res.Author); name != "" {
			ch.DisplayName = name
		}
		if title := strings.TrimSpace(res.Title); title != "" {
			ch.Title = &title
		}
		return ch, nil
	default:
		return catalog.Channel{}, err
	}
}

func uniqueTargets(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := catalog.StableID(platform.YouTube, raw, raw)
		target, err := ParseTarget(id)
		if err != nil {
			continue
		}
		if _, ok := seen[target.ID]; ok {
			continue
		}
		seen[target.ID] = struct{}{}
		out = append(out, target.ID)
	}
	return out
}

type playerResponse struct {
	PlayabilityStatus *struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"playabilityStatus"`
	VideoDetails *struct {
		VideoID       string `json:"videoId"`
		Title         string `json:"title"`
		ChannelID     string `json:"channelId"`
		Author        string `json:"author"`
		IsLive        bool   `json:"isLive"`
		IsLiveContent bool   `json:"isLiveContent"`
	} `json:"videoDetails"`
	Microformat *struct {
		PlayerMicroformatRenderer *struct {
			LiveBroadcastDetails *struct {
				IsLiveNow bool `json:"isLiveNow"`
			} `json:"liveBroadcastDetails"`
		} `json:"playerMicroformatRenderer"`
	} `json:"microformat"`
}

type initialData struct {
	Contents *struct {
		TwoColumnWatchNextResults *struct {
			Results *struct {
				Results *struct {
					Contents []struct {
						VideoPrimaryInfoRenderer *struct {
							ViewCount *struct {
								VideoViewCountRenderer *struct {
									IsLive            bool   `json:"isLive"`
									OriginalViewCount string `json:"originalViewCount"`
									ViewCount         *struct {
										SimpleText string `json:"simpleText"`
										Runs       []struct {
											Text string `json:"text"`
										} `json:"runs"`
									} `json:"viewCount"`
								} `json:"videoViewCountRenderer"`
							} `json:"viewCount"`
						} `json:"videoPrimaryInfoRenderer"`
					} `json:"contents"`
				} `json:"results"`
			} `json:"results"`
			ConversationBar *struct {
				LiveChatRenderer *struct {
					Continuations []struct {
						ReloadContinuationData *struct {
							Continuation string `json:"continuation"`
						} `json:"reloadContinuationData"`
					} `json:"continuations"`
				} `json:"liveChatRenderer"`
			} `json:"conversationBar"`
		} `json:"twoColumnWatchNextResults"`
	} `json:"contents"`
}

var (
	reCanonical     = regexp.MustCompile(`<link\s+rel=["']canonical["']\s+href=["']([^"']+)["']`)
	reAPIKey        = regexp.MustCompile(`"INNERTUBE_API_KEY":"([^"]+)"`)
	reClientVersion = regexp.MustCompile(`"INNERTUBE_CLIENT_VERSION":"([^"]+)"`)
)

func parseWatchPage(target Target, body []byte) (Resolved, error) {
	const op = "resolve"
	res := Resolved{
		Target:        target,
		ClientVersion: defaultClientVersion,
		Availability:  catalog.AvailabilityUnknown,
	}
	if m := reAPIKey.FindSubmatch(body); len(m) == 2 {
		res.APIKey = string(m[1])
	}
	if m := reClientVersion.FindSubmatch(body); len(m) == 2 && len(m[1]) > 0 {
		res.ClientVersion = string(m[1])
	}

	rawPR, hasPR := extractJSONVar(body, "ytInitialPlayerResponse")
	if !hasPR {
		// A /@channel/live request for a channel without a live stream resolves
		// to the canonical channel URL (/channel/UC...) with no player response.
		if m := reCanonical.FindSubmatch(body); len(m) == 2 {
			canon := string(m[1])
			if strings.Contains(canon, "/channel/") || strings.Contains(canon, "/@") {
				res.Availability = catalog.AvailabilityOffline
				return res, ErrOffline
			}
		}
		return res, contractError(op, errors.New("watch page missing ytInitialPlayerResponse"))
	}

	var pr playerResponse
	if err := json.Unmarshal(rawPR, &pr); err != nil {
		return res, contractError(op, fmt.Errorf("decode player response: %w", err))
	}
	if pr.VideoDetails != nil {
		res.VideoID = strings.TrimSpace(pr.VideoDetails.VideoID)
		res.ChannelID = strings.TrimSpace(pr.VideoDetails.ChannelID)
		res.Author = strings.TrimSpace(pr.VideoDetails.Author)
		res.Title = strings.TrimSpace(pr.VideoDetails.Title)
	}
	if pr.PlayabilityStatus == nil {
		return res, contractError(op, errors.New("player response missing playabilityStatus"))
	}

	status := strings.ToUpper(strings.TrimSpace(pr.PlayabilityStatus.Status))
	reason := strings.TrimSpace(pr.PlayabilityStatus.Reason)

	switch status {
	case "LOGIN_REQUIRED":
		if IsBotCheckReason(reason) {
			return res, &catalog.Error{
				Platform: platform.YouTube,
				Kind:     catalog.Bloqueio,
				Op:       op,
				Err:      fmt.Errorf("%w: %s", ErrBotCheck, reason),
			}
		}
		return res, &catalog.Error{
			Platform: platform.YouTube,
			Kind:     catalog.Bloqueio,
			Op:       op,
			Err:      errors.New(reason),
		}
	case "LIVE_STREAM_OFFLINE":
		res.Availability = catalog.AvailabilityOffline
		if reason != "" {
			return res, fmt.Errorf("%w: %s", ErrOffline, reason)
		}
		return res, ErrOffline
	case "ERROR", "UNPLAYABLE":
		if IsBotCheckReason(reason) {
			return res, &catalog.Error{
				Platform: platform.YouTube,
				Kind:     catalog.Bloqueio,
				Op:       op,
				Err:      fmt.Errorf("%w: %s", ErrBotCheck, reason),
			}
		}
		lower := strings.ToLower(reason)
		if strings.Contains(lower, "unavailable") || strings.Contains(lower, "does not exist") || strings.Contains(lower, "not available") {
			return res, ErrChannelNotFound
		}
		return res, contractError(op, fmt.Errorf("playability %s: %s", status, reason))
	case "OK":
	default:
		return res, contractError(op, fmt.Errorf("unexpected playability status %q", status))
	}

	if res.VideoID == "" {
		return res, contractError(op, errors.New("player response missing videoId"))
	}

	isLive := pr.VideoDetails != nil && pr.VideoDetails.IsLive
	if !isLive && pr.Microformat != nil && pr.Microformat.PlayerMicroformatRenderer != nil && pr.Microformat.PlayerMicroformatRenderer.LiveBroadcastDetails != nil {
		isLive = pr.Microformat.PlayerMicroformatRenderer.LiveBroadcastDetails.IsLiveNow
	}
	if !isLive {
		res.Availability = catalog.AvailabilityOffline
		return res, ErrNotLive
	}

	res.Availability = catalog.AvailabilityLive
	if rawData, ok := extractJSONVar(body, "ytInitialData"); ok {
		var idata initialData
		if err := json.Unmarshal(rawData, &idata); err == nil {
			res.Viewers = extractLiveViewers(&idata)
			res.ChatContinuation = extractReloadContinuation(&idata)
		}
	}
	return res, nil
}

// IsBotCheckReason reports whether reason contains YouTube's anti-bot challenge
// ("Sign in to confirm you're not a bot", accepting straight or curly apostrophe).
func IsBotCheckReason(reason string) bool {
	l := strings.ToLower(reason)
	return strings.Contains(l, "sign in to confirm you're not a bot") ||
		strings.Contains(l, "sign in to confirm you\u2019re not a bot")
}

func extractLiveViewers(d *initialData) *int {
	if d == nil || d.Contents == nil || d.Contents.TwoColumnWatchNextResults == nil ||
		d.Contents.TwoColumnWatchNextResults.Results == nil || d.Contents.TwoColumnWatchNextResults.Results.Results == nil {
		return nil
	}
	for _, item := range d.Contents.TwoColumnWatchNextResults.Results.Results.Contents {
		if item.VideoPrimaryInfoRenderer == nil || item.VideoPrimaryInfoRenderer.ViewCount == nil ||
			item.VideoPrimaryInfoRenderer.ViewCount.VideoViewCountRenderer == nil {
			continue
		}
		vc := item.VideoPrimaryInfoRenderer.ViewCount.VideoViewCountRenderer
		if !vc.IsLive {
			return nil
		}
		if n, ok := parseViewerNumber(vc.OriginalViewCount); ok {
			return &n
		}
		if vc.ViewCount != nil {
			var combined strings.Builder
			for _, run := range vc.ViewCount.Runs {
				combined.WriteString(run.Text)
			}
			if n, ok := parseViewerNumber(combined.String()); ok {
				return &n
			}
			if n, ok := parseViewerNumber(vc.ViewCount.SimpleText); ok {
				return &n
			}
		}
	}
	return nil
}

func parseViewerNumber(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	var digits strings.Builder
	started := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			started = true
			digits.WriteRune(r)
		case started && (r == ',' || r == '.' || r == ' ' || r == '\u00a0'):
		case started:
			goto done
		}
	}
done:
	if digits.Len() == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(digits.String())
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func extractReloadContinuation(d *initialData) string {
	if d == nil || d.Contents == nil || d.Contents.TwoColumnWatchNextResults == nil ||
		d.Contents.TwoColumnWatchNextResults.ConversationBar == nil ||
		d.Contents.TwoColumnWatchNextResults.ConversationBar.LiveChatRenderer == nil {
		return ""
	}
	for _, c := range d.Contents.TwoColumnWatchNextResults.ConversationBar.LiveChatRenderer.Continuations {
		if c.ReloadContinuationData != nil && strings.TrimSpace(c.ReloadContinuationData.Continuation) != "" {
			return strings.TrimSpace(c.ReloadContinuationData.Continuation)
		}
	}
	return ""
}

// extractJSONVar locates `var <name> = { ... }` in HTML and returns the balanced
// JSON object bytes without relying on fragile regex terminators.
func extractJSONVar(src []byte, varName string) ([]byte, bool) {
	patterns := [][]byte{
		[]byte("var " + varName + " ="),
		[]byte("var " + varName + "="),
		[]byte(`window["` + varName + `"] =`),
		[]byte(`window["` + varName + `"]=`),
	}
	idx := -1
	offset := 0
	for _, p := range patterns {
		if i := bytes.Index(src, p); i >= 0 {
			idx = i
			offset = len(p)
			break
		}
	}
	if idx < 0 {
		return nil, false
	}
	rest := src[idx+offset:]
	startRel := bytes.IndexByte(rest, '{')
	if startRel < 0 {
		return nil, false
	}
	obj := rest[startRel:]
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(obj); i++ {
		b := obj[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if b == '\\' {
				escaped = true
				continue
			}
			if b == '"' {
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return obj[:i+1], true
			}
		}
	}
	return nil, false
}

func statusError(op string, status int, header http.Header, now time.Time, notFound error) error {
	e := &catalog.Error{
		Platform:   platform.YouTube,
		Op:         op,
		StatusCode: status,
		Kind:       catalog.HTTPStatusKind(status),
	}
	switch status {
	case http.StatusForbidden:
		e.Kind = catalog.Bloqueio
		e.Err = errors.New("request refused by YouTube")
	case http.StatusNotFound:
		e.Err = notFound
	case http.StatusTooManyRequests:
		if resetAfter, resetAt, ok := parseRetryAfter(header, now); ok {
			e.ResetAfter = resetAfter
			e.ResetAt = resetAt
		}
	}
	return e
}

func parseRetryAfter(header http.Header, now time.Time) (time.Duration, time.Time, bool) {
	if header == nil {
		return 0, time.Time{}, false
	}
	raw := strings.TrimSpace(header.Get("Retry-After"))
	if raw == "" {
		return 0, time.Time{}, false
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs <= 0 {
			return 0, time.Time{}, false
		}
		d := time.Duration(secs) * time.Second
		if now.IsZero() {
			now = time.Now()
		}
		return d, now.Add(d), true
	}
	if at, err := http.ParseTime(raw); err == nil {
		if now.IsZero() {
			now = time.Now()
		}
		if d := at.Sub(now); d > 0 {
			return d, at, true
		}
	}
	return 0, time.Time{}, false
}

func contractError(op string, err error) error {
	return &catalog.Error{
		Platform: platform.YouTube,
		Kind:     catalog.Contrato,
		Op:       op,
		Err:      err,
	}
}
