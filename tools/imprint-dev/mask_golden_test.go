package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestMaskGolden checks MaskDetail against the golden corpus of the mask
// parity spec (section 8, 2026-10-02): tools/typesafe/tests/mask-golden.json,
// a copy of typesafe-dev's file, written by its tests/gen_mask_golden.py from
// ts_common.mask_detail (Python) and checked there by
// tests/test_mask_golden.py. Every case must give exactly its "masked" text
// and all seven "counts", with TYPESAFE_NAMES_FILE holding exactly the
// header's "names", one per line (synthetic; the private names file is never
// read). A file with no cases, or with a case count other than the header's,
// fails. The header's unidata_version must be nfcUnicodeVersion: Go's NFC
// tables are generated from the same unicodedata (spec section 6), so the two
// agree or the corpus was made with another Python. Unlike the Python side,
// which skips on another unicodedata, this test runs without python3.
func TestMaskGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "typesafe", "tests", "mask-golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Header struct {
			Generator      string   `json:"generator"`
			Date           string   `json:"date"`
			Python         string   `json:"python"`
			UnidataVersion string   `json:"unidata_version"`
			CaseCount      int      `json:"case_count"`
			Categories     []string `json:"categories"`
			Names          []string `json:"names"`
		} `json:"header"`
		Cases []struct {
			ID     string         `json:"id"`
			Text   string         `json:"text"`
			Masked string         `json:"masked"`
			Counts map[string]int `json:"counts"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	h := golden.Header
	if len(golden.Cases) == 0 {
		t.Fatal("mask-golden.json holds no cases")
	}
	if len(golden.Cases) != h.CaseCount {
		t.Fatalf("mask-golden.json holds %d cases, its header says %d", len(golden.Cases), h.CaseCount)
	}
	t.Logf("header: %s, %s, Python %s, unidata_version %s; nfcUnicodeVersion %s", h.Generator, h.Date, h.Python,
		h.UnidataVersion, nfcUnicodeVersion)
	if h.UnidataVersion != nfcUnicodeVersion {
		t.Errorf("mask-golden.json was made with unicodedata %s, nfc_tables.go with %s: regenerate one of them with a Python whose unicodedata is the other's",
			h.UnidataVersion, nfcUnicodeVersion)
	}
	// The seven categories: MaskCounts' JSON field names, the header's in
	// Python's order (DEFAULT_CATEGORIES).
	var categories []string
	ct := reflect.TypeFor[MaskCounts]()
	for i := 0; i < ct.NumField(); i++ {
		categories = append(categories, ct.Field(i).Tag.Get("json"))
	}
	if a, b := slices.Sorted(slices.Values(h.Categories)), slices.Sorted(slices.Values(categories)); !slices.Equal(a, b) {
		t.Fatalf("header categories %v, MaskCounts has %v", h.Categories, categories)
	}
	if len(h.Names) == 0 {
		t.Fatal("header holds no names")
	}
	nf := filepath.Join(t.TempDir(), "names.txt")
	if err := os.WriteFile(nf, []byte(strings.Join(h.Names, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TYPESAFE_NAMES_FILE", nf)

	failures := 0
	for _, c := range golden.Cases {
		if len(c.Counts) != len(categories) {
			t.Fatalf("%s: %d counts, want all %d: %v", c.ID, len(c.Counts), len(categories), c.Counts)
		}
		var want MaskCounts
		b, _ := json.Marshal(c.Counts)
		if err := json.Unmarshal(b, &want); err != nil {
			t.Fatal(err)
		}
		for _, cat := range categories {
			if _, ok := c.Counts[cat]; !ok {
				t.Fatalf("%s: no count for %s: %v", c.ID, cat, c.Counts)
			}
		}
		got, counts := MaskDetail(c.Text)
		if got != c.Masked || counts != want {
			if failures++; failures <= 10 {
				t.Errorf("%s: text %+q\n want %+q %+v\n  got %+q %+v", c.ID, c.Text, c.Masked, want, got, counts)
			}
		}
	}
	if failures > 0 {
		t.Errorf("%d of %d cases differ", failures, len(golden.Cases))
		return
	}
	t.Logf("%d cases, %d names: masked text and all %d counts as in the file", len(golden.Cases), len(h.Names), len(categories))
}
