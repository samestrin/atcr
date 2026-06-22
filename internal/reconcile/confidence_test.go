package reconcile

import "testing"

func TestConfidenceAtOrAbove(t *testing.T) {
	cases := []struct {
		conf  string
		floor string
		want  bool
	}{
		{ConfidenceVerified, ConfHigh, true}, // VERIFIED is above HIGH
		{ConfHigh, ConfHigh, true},           // equal
		{ConfMedium, ConfHigh, false},        // below
		{ConfLow, ConfHigh, false},           // below
		{ConfHigh, ConfMedium, true},         // above
		{"verified", ConfHigh, true},         // case-insensitive
		{"  HIGH  ", ConfHigh, true},         // whitespace-insensitive
		{"BOGUS", ConfHigh, false},           // unknown finding confidence fails closed
		{ConfHigh, "BOGUS", false},           // unknown floor fails closed
		{"", ConfHigh, false},                // empty fails closed
	}
	for _, c := range cases {
		if got := ConfidenceAtOrAbove(c.conf, c.floor); got != c.want {
			t.Errorf("ConfidenceAtOrAbove(%q,%q)=%v want %v", c.conf, c.floor, got, c.want)
		}
	}
}
