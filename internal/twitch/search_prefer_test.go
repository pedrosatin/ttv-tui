package twitch

import (
	"strings"
	"testing"
)

func TestPreferSearchChannelsExactFirstAmongOfflineClones(t *testing.T) {
	// Twitch relevance often ranks a clone ahead of the exact login.
	items := []Channel{
		{ID: "1", Login: "landonorris0481", DisplayName: "landonorris0481", Availability: AvailabilityOffline},
		{ID: "2", Login: "landonorris", DisplayName: "LandoNorris", Availability: AvailabilityOffline},
		{ID: "3", Login: "landonorris16", DisplayName: "landonorris16", Availability: AvailabilityOffline},
	}
	got := PreferSearchChannels("landonorris", items)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if !strings.EqualFold(got[0].Login, "landonorris") {
		t.Fatalf("first = %q, want exact login landonorris", got[0].Login)
	}
	// Remaining keep Twitch order among equal preference.
	if got[1].Login != "landonorris0481" || got[2].Login != "landonorris16" {
		t.Fatalf("tail = %q, %q; want original clone order", got[1].Login, got[2].Login)
	}
}

func TestPreferSearchChannelsExactOfflineBeatsLiveClone(t *testing.T) {
	// Chosen rule: exact login stays first even when offline and a clone is live.
	items := []Channel{
		{ID: "clone", Login: "landonorris0481", DisplayName: "landonorris0481", Availability: AvailabilityLive},
		{ID: "exact", Login: "landonorris", DisplayName: "LandoNorris", Availability: AvailabilityOffline},
	}
	got := PreferSearchChannels("  LandONorris  ", items)
	if got[0].Login != "landonorris" {
		t.Fatalf("first = %q, want exact landonorris (exact beats live clone)", got[0].Login)
	}
	if got[1].Login != "landonorris0481" || got[1].Availability != AvailabilityLive {
		t.Fatalf("second = %+v, want live clone", got[1])
	}
}

func TestPreferSearchChannelsLiveBeforeOfflineAmongNonExact(t *testing.T) {
	items := []Channel{
		{ID: "a", Login: "foo_off", DisplayName: "foo_off", Availability: AvailabilityOffline},
		{ID: "b", Login: "foo_live", DisplayName: "foo_live", Availability: AvailabilityLive},
		{ID: "c", Login: "foo_unk", DisplayName: "foo_unk", Availability: AvailabilityUnknown},
	}
	got := PreferSearchChannels("other", items)
	if got[0].Login != "foo_live" {
		t.Fatalf("first non-exact = %q, want live foo_live", got[0].Login)
	}
	// Offline and unknown keep relative order after the live channel.
	if got[1].Login != "foo_off" || got[2].Login != "foo_unk" {
		t.Fatalf("tail = %q, %q; want offline then unknown in input order", got[1].Login, got[2].Login)
	}
}

func TestPreferSearchChannelsEmptyQueryAndEmptyItems(t *testing.T) {
	if got := PreferSearchChannels("", nil); got != nil {
		t.Fatalf("nil items: got %#v", got)
	}
	one := []Channel{{ID: "1", Login: "only", DisplayName: "Only"}}
	if got := PreferSearchChannels("only", one); len(got) != 1 || got[0].Login != "only" {
		t.Fatalf("single item: got %#v", got)
	}
	items := []Channel{
		{ID: "1", Login: "a", DisplayName: "A", Availability: AvailabilityOffline},
		{ID: "2", Login: "b", DisplayName: "B", Availability: AvailabilityLive},
	}
	got := PreferSearchChannels("   ", items)
	if got[0].Login != "b" {
		t.Fatalf("blank query should still prefer live, first = %q", got[0].Login)
	}
}

func TestPreferSearchChannelsDoesNotMutateInput(t *testing.T) {
	items := []Channel{
		{ID: "1", Login: "clone", DisplayName: "clone", Availability: AvailabilityOffline},
		{ID: "2", Login: "exact", DisplayName: "exact", Availability: AvailabilityOffline},
	}
	_ = PreferSearchChannels("exact", items)
	if items[0].Login != "clone" {
		t.Fatalf("input mutated: first = %q", items[0].Login)
	}
}
