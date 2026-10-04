package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write puts a .go file into dir, creating dir if needed.
func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func twoSides(t *testing.T, local, kit string) (string, string) {
	t.Helper()
	base := t.TempDir()
	ld, kd := filepath.Join(base, "local"), filepath.Join(base, "kit")
	write(t, ld, "a.go", local)
	write(t, kd, "a.go", kit)
	return ld, kd
}

func TestTagDifferenceIsDetected(t *testing.T) {
	ld, kd := twoSides(t, `package dto

type T struct {
	Delta string `+"`"+`json:"delta,omitempty"`+"`"+`
}
`, `package dto

type T struct {
	Delta string `+"`"+`json:"delta"`+"`"+`
}
`)
	r, err := build(ld, kd)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.tagDiff) != 1 {
		t.Fatalf("want 1 tag diff, got %d: %v", len(r.tagDiff), r.tagDiff)
	}
	if !strings.Contains(r.tagDiff[0], "delta,omitempty") {
		t.Errorf("tag diff missing local tag: %v", r.tagDiff[0])
	}
}

func TestTypeDifferenceIsDetected(t *testing.T) {
	ld, kd := twoSides(t, `package dto

type T struct {
	Text string `+"`"+`json:"text"`+"`"+`
}
`, `package dto

type T struct {
	Text *string `+"`"+`json:"text"`+"`"+`
}
`)
	r, err := build(ld, kd)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.typeDiff) != 1 {
		t.Fatalf("want 1 type diff, got %d: %v", len(r.typeDiff), r.typeDiff)
	}
	if len(r.tagDiff) != 0 {
		t.Errorf("a type change must not be reported as a tag change: %v", r.tagDiff)
	}
}

// interface{} and any denote the same type; they must be reported as a
// spelling difference, not as a type difference.
func TestInterfaceAnyIsSpellingNotTypeDiff(t *testing.T) {
	ld, kd := twoSides(t, `package dto

type T struct {
	V interface{} `+"`"+`json:"v"`+"`"+`
}
`, `package dto

type T struct {
	V any `+"`"+`json:"v"`+"`"+`
}
`)
	r, err := build(ld, kd)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.typeDiff) != 0 {
		t.Fatalf("interface{}/any must not count as a type diff, got %v", r.typeDiff)
	}
	if len(r.tagEquivalents) != 1 {
		t.Fatalf("want the spelling recorded once, got %v", r.tagEquivalents)
	}
}

// An embedded field is invisible to a field-name diff (ast.Field.Names is
// empty), so it needs its own dimension.
func TestEmbeddedFieldDifferenceIsDetected(t *testing.T) {
	ld, kd := twoSides(t, `package dto

type Usage struct{ N int }

type T struct {
	Usage `+"`"+`json:"usage"`+"`"+`
	Name  string `+"`"+`json:"name"`+"`"+`
}
`, `package dto

type Usage struct{ N int }

type T struct {
	Name string `+"`"+`json:"name"`+"`"+`
}
`)
	r, err := build(ld, kd)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.embedDiff) != 1 {
		t.Fatalf("want 1 embed diff, got %d: %v", len(r.embedDiff), r.embedDiff)
	}
}

func TestSignatureDifferenceIsDetected(t *testing.T) {
	ld, kd := twoSides(t, `package dto

type C struct{}

func (c *C) IsStream(ctx any) bool { return false }
`, `package dto

type C struct{}

func (c *C) IsStream(ctx *C) bool { return false }
`)
	r, err := build(ld, kd)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.sigDiff) != 1 {
		t.Fatalf("want 1 signature diff, got %d: %v", len(r.sigDiff), r.sigDiff)
	}
}

// A field present on one side only belongs to the name dimension.
func TestOneSidedFieldIsNameDiff(t *testing.T) {
	ld, kd := twoSides(t, `package dto

type T struct {
	A string `+"`"+`json:"a"`+"`"+`
	B string `+"`"+`json:"b"`+"`"+`
}
`, `package dto

type T struct {
	A string `+"`"+`json:"a"`+"`"+`
}
`)
	r, err := build(ld, kd)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.fieldSetDiff) != 1 {
		t.Fatalf("want 1 name diff, got %d: %v", len(r.fieldSetDiff), r.fieldSetDiff)
	}
	if len(r.tagDiff) != 0 || len(r.typeDiff) != 0 {
		t.Errorf("a one-sided field must not leak into tag/type: %v %v", r.tagDiff, r.typeDiff)
	}
}

// Test files must be ignored; they are not part of the compatibility surface.
func TestTestFilesIgnored(t *testing.T) {
	ld, kd := twoSides(t, `package dto

type T struct{}
`, `package dto

type T struct{}
`)
	write(t, ld, "a_test.go", "package dto\n\nfunc helperOnlyInLocal() {}\n")
	r, err := build(ld, kd)
	if err != nil {
		t.Fatal(err)
	}
	if r.sharedFuncs != 0 {
		t.Errorf("test files must not contribute functions, got %d", r.sharedFuncs)
	}
}
