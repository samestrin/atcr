package reconcile

import (
	"reflect"
	"strings"
	"testing"

	reclib "github.com/samestrin/atcr/reconcile"
)

// siblingFields walks t (and any struct it reaches through fields, pointers,
// slices or maps) and returns every field whose name contains "sibling"
// (case-insensitive) or whose json tag contains "sibling".
func siblingFields(t reflect.Type) []string {
	var found []string
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || seen[t] {
			return
		}
		seen[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if strings.Contains(strings.ToLower(f.Name), "sibling") ||
				strings.Contains(strings.ToLower(f.Tag.Get("json")), "sibling") {
				found = append(found, t.Name()+"."+f.Name)
			}
			walk(f.Type)
		}
	}
	walk(t)
	return found
}

// TestReclibFinding_HasNoSiblingLocationField pins AC4 of Epic 35.16.15: the
// cross-branch routing marker lives on the in-tree JSONFinding only, and the
// published reconcile module's Finding stays free of it.
func TestReclibFinding_HasNoSiblingLocationField(t *testing.T) {
	if got := siblingFields(reflect.TypeOf(reclib.Finding{})); len(got) != 0 {
		t.Fatalf("reclib.Finding carries sibling-location fields %v; the marker belongs on JSONFinding only", got)
	}
}

// TestSiblingFields_DetectsSiblingField proves the check above can fail: a type
// with a sibling field, by name or by json tag, at the top level or nested,
// is reported.
func TestSiblingFields_DetectsSiblingField(t *testing.T) {
	type nested struct {
		Where string `json:"sibling_location"`
	}
	type withSibling struct {
		File            string  `json:"file"`
		SiblingLocation string  `json:"x,omitempty"`
		Next            *nested `json:"next,omitempty"`
	}
	got := siblingFields(reflect.TypeOf(withSibling{}))
	want := []string{"withSibling.SiblingLocation", "nested.Where"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("siblingFields = %v, want %v", got, want)
	}
}
