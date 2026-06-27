package tdmigrate

import "testing"

func TestCheckboxToStatus_KnownTokens(t *testing.T) {
	cases := map[string]string{
		"[ ]": StatusOpen,
		"[/]": StatusDeferred,
		"[x]": StatusResolved,
		"[X]": StatusResolved,
	}
	for box, want := range cases {
		got, err := CheckboxToStatus(box)
		if err != nil {
			t.Fatalf("CheckboxToStatus(%q) unexpected error: %v", box, err)
		}
		if got != want {
			t.Errorf("CheckboxToStatus(%q) = %q, want %q", box, got, want)
		}
	}
}

func TestCheckboxToStatus_Unknown(t *testing.T) {
	if _, err := CheckboxToStatus("[?]"); err == nil {
		t.Error("expected error for unknown checkbox token")
	}
}

func TestStatusToCheckbox_RoundTrip(t *testing.T) {
	for _, status := range []string{StatusOpen, StatusDeferred, StatusResolved} {
		box, err := StatusToCheckbox(status)
		if err != nil {
			t.Fatalf("StatusToCheckbox(%q) error: %v", status, err)
		}
		got, err := CheckboxToStatus(box)
		if err != nil {
			t.Fatalf("CheckboxToStatus(%q) error: %v", box, err)
		}
		if got != status {
			t.Errorf("round-trip status %q -> %q -> %q", status, box, got)
		}
	}
}

func TestStatusToCheckbox_Unknown(t *testing.T) {
	if _, err := StatusToCheckbox("bogus"); err == nil {
		t.Error("expected error for unknown status")
	}
}

func TestNormalizeSeverity(t *testing.T) {
	if got := NormalizeSeverity("  low "); got != "LOW" {
		t.Errorf("NormalizeSeverity = %q, want LOW", got)
	}
}

func validItem() Item {
	return Item{
		Group:      "1",
		Status:     StatusDeferred,
		Severity:   "LOW",
		File:       "internal/foo.go:10",
		Problem:    "something is off",
		Fix:        "fix it",
		Category:   "correctness",
		EstMinutes: 30,
		Source:     "code-review",
	}
}

func TestItemValidate_OK(t *testing.T) {
	if err := validItem().Validate(); err != nil {
		t.Fatalf("valid item rejected: %v", err)
	}
}

func TestItemValidate_Rejections(t *testing.T) {
	mutators := map[string]func(*Item){
		"bad severity":   func(i *Item) { i.Severity = "URGENT" },
		"bad status":     func(i *Item) { i.Status = "wip" },
		"empty file":     func(i *Item) { i.File = "" },
		"empty problem":  func(i *Item) { i.Problem = "" },
		"empty fix":      func(i *Item) { i.Fix = "" },
		"empty category": func(i *Item) { i.Category = "" },
		"empty source":   func(i *Item) { i.Source = "" },
		"empty group":    func(i *Item) { i.Group = "" },
		"negative est":   func(i *Item) { i.EstMinutes = -1 },
	}
	for name, mut := range mutators {
		it := validItem()
		mut(&it)
		if err := it.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

func TestItemValidateFileFormat(t *testing.T) {
	valid := validItem()
	if err := valid.ValidateFileFormat(); err != nil {
		t.Errorf("valid file rejected: %v", err)
	}

	bad := validItem()
	bad.File = "just-a-path"
	if err := bad.ValidateFileFormat(); err == nil {
		t.Error("expected error for file without :line suffix")
	}
}

func TestItemValidate_RequiredFieldOrderIsDeterministic(t *testing.T) {
	base := validItem()
	base.File = ""
	base.Problem = ""

	var first string
	for i := 0; i < 20; i++ {
		err := base.Validate()
		if err == nil {
			t.Fatal("expected validation error")
		}
		msg := err.Error()
		if i == 0 {
			first = msg
		} else if msg != first {
			t.Fatalf("required-field validation order is non-deterministic: %q vs %q", first, msg)
		}
	}
}

func TestShardValidate_OK(t *testing.T) {
	s := Shard{Date: "2026-06-26", SourceType: "Sprint", Label: "epic-1.0", Items: []Item{validItem()}}
	if err := s.Validate(); err != nil {
		t.Fatalf("valid shard rejected: %v", err)
	}
}

func TestShardValidate_Rejections(t *testing.T) {
	base := func() Shard {
		return Shard{Date: "2026-06-26", SourceType: "Review", Label: "x", Items: []Item{validItem()}}
	}
	mutators := map[string]func(*Shard){
		"empty date":      func(s *Shard) { s.Date = "" },
		"bad source type": func(s *Shard) { s.SourceType = "Cron" },
		"empty label":     func(s *Shard) { s.Label = "" },
		"no items":        func(s *Shard) { s.Items = nil },
		"bad nested item": func(s *Shard) { s.Items[0].Severity = "nope" },
	}
	for name, mut := range mutators {
		s := base()
		mut(&s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}
