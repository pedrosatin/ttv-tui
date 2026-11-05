package chat

import "testing"

func TestIsBlockedCommand(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{name: "ban", body: "/ban Evasor spam", want: true},
		{name: "no_args", body: "/clear", want: true},
		{name: "bare_slash", body: "/", want: true},
		{name: "leading_spaces", body: "  /ban x", want: true},
		{name: "leading_tab", body: "\t/ban x", want: true},
		{name: "leading_nbsp", body: " /ban x", want: true},
		{name: "leading_nul_becomes_space_on_wire", body: "\x00/ban x", want: true},
		{name: "starts_with_me_but_is_another_word", body: "/mean x", want: true},
		{name: "uppercase_me_is_not_the_known_form", body: "/ME oi", want: true},
		{name: "subreddit_at_start", body: "/r/golang", want: true},

		{name: "me", body: "/me oi", want: false},
		{name: "me_double_space", body: "/me  texto", want: false},
		{name: "me_tab", body: "/me\ttexto", want: false},
		{name: "me_leading_spaces", body: "  /me oi", want: false},
		{name: "bare_me", body: "/me", want: false},

		{name: "empty", body: "", want: false},
		{name: "spaces", body: "   ", want: false},
		{name: "plain", body: "oi chat", want: false},
		{name: "fraction", body: "veja 1/2", want: false},
		{name: "url", body: "http://x/y", want: false},
		{name: "url_mid_message", body: "veja http://x/y", want: false},
		{name: "command_mid_message", body: "use /ban aqui", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsBlockedCommand(tc.body); got != tc.want {
				t.Fatalf("IsBlockedCommand(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}
