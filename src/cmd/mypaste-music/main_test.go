package main

import (
	"strings"
	"testing"
)

func TestRecentSearchesKeepsTheNewestFive(t *testing.T) {
	kept := recentSearches([]string{
		" 晴天 ", "稻香", "", "晴天", "七里香", "夜曲", "以父之名", "告白气球",
	})
	want := []string{"晴天", "稻香", "七里香", "夜曲", "以父之名"}
	if len(kept) != len(want) {
		t.Fatalf("kept %d queries, want %d: %q", len(kept), len(want), kept)
	}
	for i := range want {
		if kept[i] != want[i] {
			t.Fatalf("query %d = %q, want %q", i, kept[i], want[i])
		}
	}
}

func TestRecentSearchesDropsAnOversizedKeyword(t *testing.T) {
	kept := recentSearches([]string{
		strings.Repeat("字", maxKeywordRunes+1), "稻香",
	})
	if len(kept) != 1 || kept[0] != "稻香" {
		t.Fatalf("kept %q, want [稻香]", kept)
	}
}

func TestDefaultStateStartsWithAnEmptySearchHistory(t *testing.T) {
	if got := defaultState().Searches; got == nil || len(got) != 0 {
		t.Fatalf("default searches = %#v, want an empty list", got)
	}
}
