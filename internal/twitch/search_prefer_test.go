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

func TestPreferSearchChannelsExactDisplayNameBeatsLiveClone(t *testing.T) {
	items := []Channel{
		{ID: "clone", Login: "fan_page", DisplayName: "fan_page", Availability: AvailabilityLive},
		{ID: "name", Login: "koreanstreamer", DisplayName: "한글이름", Availability: AvailabilityOffline},
	}
	got := PreferSearchChannels(" 한글이름 ", items)
	if got[0].ID != "name" {
		t.Fatalf("first = %q, want exact display name", got[0].Login)
	}
}

func TestPreferSearchChannelsExactLoginBeatsExactDisplayName(t *testing.T) {
	items := []Channel{
		{ID: "name", Login: "someoneelse", DisplayName: "Gaules", Availability: AvailabilityLive},
		{ID: "login", Login: "gaules", DisplayName: "TheRealOne", Availability: AvailabilityOffline},
	}
	got := PreferSearchChannels("gaules", items)
	if got[0].ID != "login" || got[1].ID != "name" {
		t.Fatalf("order = %q, %q; want login match then display name", got[0].ID, got[1].ID)
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

func TestPreferSearchCategoriesExactThenViewers(t *testing.T) {
	// Twitch relevance may put a partial match ahead of the exact name.
	items := []Category{
		{ID: "2", Name: "Teamfight Manager 2", Viewers: IntPtr(3)},
		{ID: "1", Name: "Teamfight Tactics", Viewers: IntPtr(4063)},
		{ID: "3", Name: "Teamfight Manager", Viewers: nil},
	}
	got := PreferSearchCategories("teamfight tactics", items)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Name != "Teamfight Tactics" {
		t.Fatalf("first = %q, want exact Teamfight Tactics", got[0].Name)
	}
	if got[1].Name != "Teamfight Manager 2" {
		t.Fatalf("second = %q, want higher-viewers Teamfight Manager 2", got[1].Name)
	}
	if got[2].Name != "Teamfight Manager" || got[2].Viewers != nil {
		t.Fatalf("third = %+v, want null-viewers Teamfight Manager last", got[2])
	}
}

func TestPreferSearchCategoriesZeroViewersLast(t *testing.T) {
	items := []Category{
		{ID: "z", Name: "Zero Game", Viewers: IntPtr(0)},
		{ID: "a", Name: "Active Game", Viewers: IntPtr(10)},
		{ID: "n", Name: "Null Game", Viewers: nil},
	}
	got := PreferSearchCategories("game", items)
	if got[0].Name != "Active Game" {
		t.Fatalf("first = %q, want Active Game", got[0].Name)
	}
	// Zero and nil stay after positive viewers; relative order among them kept.
	if got[1].Name != "Zero Game" || got[2].Name != "Null Game" {
		t.Fatalf("tail = %q, %q; want zero then null in input order", got[1].Name, got[2].Name)
	}
}

func TestPreferSearchCategoriesDoesNotMutateInput(t *testing.T) {
	items := []Category{
		{ID: "1", Name: "B", Viewers: IntPtr(1)},
		{ID: "2", Name: "A", Viewers: IntPtr(9)},
	}
	_ = PreferSearchCategories("a", items)
	if items[0].Name != "B" {
		t.Fatalf("input mutated: first = %q", items[0].Name)
	}
}
