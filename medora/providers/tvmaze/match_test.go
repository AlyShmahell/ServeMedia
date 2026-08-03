package tvmaze

import "testing"

func TestPickBestShowExactName(t *testing.T) {
	hits := []searchHit{
		{Score: 2.0, Show: show{ID: 1, Name: "Token"}},
		{Score: 1.0, Show: show{ID: 2, Name: "The Token Overlap Show", Type: "Animation", Genres: []string{"Anime"}}},
	}
	got, ok := pickBestShow("The Token Overlap Show", 0, "anime", hits)
	if !ok || got.ID != 2 {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestPickBestShowArticleStripExact(t *testing.T) {
	hits := []searchHit{
		{Score: 0.9, Show: show{ID: 1, Name: "Region: Token Land", Type: "Documentary"}},
		{Score: 0.8, Show: show{ID: 2, Name: "The Token Overlap Show", Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese"}},
	}
	got, ok := pickBestShow("Token Overlap Show", 0, "anime", hits)
	if !ok || got.ID != 2 {
		t.Fatalf("got %#v ok=%v want The Token Overlap Show", got, ok)
	}
}

func TestPickBestShowRejectsPartialTokenMatch(t *testing.T) {
	hits := []searchHit{
		{Score: 5.0, Show: show{ID: 1, Name: "Region: Token Land", Type: "Documentary"}},
	}
	_, ok := pickBestShow("Token Overlap Show", 0, "anime", hits)
	if ok {
		t.Fatal("expected no match when overlap token missing")
	}
}

func TestPickBestShowAnimeBias(t *testing.T) {
	hits := []searchHit{
		{Score: 2.0, Show: show{ID: 1, Name: "Series Alpha Extra", Type: "Documentary", Language: "English"}},
		{Score: 1.5, Show: show{ID: 2, Name: "Series Alpha", Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese"}},
	}
	got, ok := pickBestShow("Series Alpha", 0, "anime", hits)
	if !ok || got.ID != 2 {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestPickBestShowScoreFallback(t *testing.T) {
	hits := []searchHit{
		{Score: 0.5, Show: show{ID: 1, Name: "Totally Unrelated"}},
		{Score: 3.0, Show: show{ID: 2, Name: "Token Overlap Show Adventures", Type: "Animation", Genres: []string{"Anime"}}},
	}
	got, ok := pickBestShow("Token Overlap Show", 0, "anime", hits)
	if !ok || got.ID != 2 {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestPickBestShowEmpty(t *testing.T) {
	_, ok := pickBestShow("x", 0, "tv", nil)
	if ok {
		t.Fatal("expected no match")
	}
}

func TestPickBestShowRomanizedEnglishTitle(t *testing.T) {
	hits := []searchHit{
		{Score: 1.2, Show: show{
			ID: 10, Name: "Romanized Listing A Zorblen",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
		{Score: 0.4, Show: show{ID: 9, Name: "Unrelated Fantasy", Type: "Scripted"}},
	}
	got, ok := pickBestShow("Zorblen of Fantasy and Ash", 0, "anime", hits)
	if !ok || got.ID != 10 {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestEnoughTokensPresent(t *testing.T) {
	if !enoughTokensPresent("Zorblen of Fantasy and Ash", "Romanized Listing A Zorblen", true) {
		t.Fatal("expected romanized anime proper-name match")
	}
	if enoughTokensPresent("Zorblen of Fantasy and Ash", "Romanized Listing A Zorblen", false) {
		t.Fatal("romanized match requires animeCand")
	}
	if enoughTokensPresent("Token Overlap Show", "Region: Token Land", false) {
		t.Fatal("token alone must not match Overlap query")
	}
	if enoughTokensPresent("The Series Beta Bride", "Series Documentaries", false) {
		t.Fatal("series alone must not match Beta Bride")
	}
	if enoughTokensPresent("The Multi Token Are You Short Me", "Short", false) {
		t.Fatal("short alone must not match multi-token query")
	}
	if enoughTokensPresent("vanished", "Epithet Vanished", true) {
		t.Fatal("single-token must be exact titleKey, not superset")
	}
	if !enoughTokensPresent("vanished", "Vanished", true) {
		t.Fatal("exact single-token should match")
	}
}

func TestShortenedQuery(t *testing.T) {
	got := shortenedQuery("Zorblen of Fantasy and Ash")
	if got != "zorblen fantasy ash" {
		t.Fatalf("shortened: %q", got)
	}
}

func TestPickBestShowRejectsShortTokenHit(t *testing.T) {
	hits := []searchHit{
		{Score: 5.0, Show: show{ID: 1, Name: "Short", Type: "Scripted"}},
		{Score: 1.0, Show: show{
			ID: 42, Name: "The Multi Token? Are You Short Me?",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
	}
	got, ok := pickBestShow("The Multi Token Are You Short Me", 0, "anime", hits)
	if !ok || got.ID != 42 {
		t.Fatalf("got %#v ok=%v want multi-token anime, not Short", got, ok)
	}
}

func TestPickBestShowRejectsPartialSharedToken(t *testing.T) {
	hits := []searchHit{
		{Score: 4.0, Show: show{ID: 1, Name: "Series Documentaries", Type: "Documentary"}},
		{Score: 2.0, Show: show{
			ID: 2, Name: "Romanized Listing B",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
		{Score: 1.5, Show: show{
			ID: 3, Name: "The Series Beta Bride",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
	}
	got, ok := pickBestShow("The Series Beta Bride", 0, "anime", hits)
	if !ok || got.ID != 3 {
		t.Fatalf("got %#v ok=%v want Series Beta Bride", got, ok)
	}
}

func TestPickBestShowJapaneseListingOnly(t *testing.T) {
	hits := []searchHit{
		{Score: 0.53, Show: show{
			ID: 11, Name: "Romanized Listing B",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
	}
	got, ok := pickBestShow("The Series Beta Bride", 0, "anime", hits)
	if !ok || got.ID != 11 {
		t.Fatalf("got %#v ok=%v want romanized anime listing", got, ok)
	}
}

func TestPickBestShowRejectsAltSearchLiveAction(t *testing.T) {
	hits := []searchHit{
		{Score: 0.89, Show: show{ID: 1, Name: "Short", Type: "Scripted"}},
		{Score: 0.52, Show: show{
			ID: 12, Name: "Romanized Listing C",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
	}
	_, ok := pickBestShow("The Multi Token Are You Short Me", 0, "anime", hits)
	if ok {
		t.Fatal("must not accept anime fallback when top search hit is not anime")
	}
}

func TestPickBestShowMultiTokenJapaneseListing(t *testing.T) {
	hits := []searchHit{
		{Score: 0.52, Show: show{
			ID: 12, Name: "Romanized Listing C",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
	}
	got, ok := pickBestShow("The Multi Token Are You Short Me", 0, "anime", hits)
	if !ok || got.ID != 12 {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestPickBestShowSingleTokenPrefersAnime(t *testing.T) {
	hits := []searchHit{
		{Score: 0.63, Show: show{ID: 1, Name: "History Vanished", Type: "Documentary"}},
		{Score: 0.53, Show: show{ID: 2, Name: "Romanized Live Action", Type: "Scripted", Language: "Japanese"}},
		{Score: 0.52, Show: show{
			ID: 13, Name: "Romanized Anime Title",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
		{Score: 0.50, Show: show{ID: 4, Name: "Epithet Vanished", Type: "Animation"}},
	}
	got, ok := pickBestShow("Vanished", 0, "anime", hits)
	if !ok || got.ID != 13 {
		t.Fatalf("got %#v ok=%v want anime romanized listing", got, ok)
	}
}

func TestPickBestShowPrefersAnimationType(t *testing.T) {
	hits := []searchHit{
		{Score: 2.0, Show: show{ID: 1, Name: "Series Gamma and Companions", Type: "Scripted", Language: "Japanese"}},
		{Score: 1.5, Show: show{
			ID: 2, Name: "Series Gamma and Companions",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
	}
	got, ok := pickBestShow("Series Gamma and Companions", 0, "anime", hits)
	if !ok || got.ID != 2 {
		t.Fatalf("got %#v ok=%v want Animation", got, ok)
	}
}

func TestPickBestShowSharedTokenJapaneseListing(t *testing.T) {
	hits := []searchHit{
		{Score: 0.51, Show: show{
			ID: 14, Name: "Series Gamma to N Companions",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
	}
	got, ok := pickBestShow("Series Gamma and Companions", 0, "anime", hits)
	if !ok || got.ID != 14 {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestPickBestShowRejectsShortPrefix(t *testing.T) {
	hits := []searchHit{
		{Score: 3.0, Show: show{ID: 1, Name: "The Prefixes", Type: "Scripted"}},
		{Score: 1.2, Show: show{
			ID: 2, Name: "The Prefix at Academy Campus",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
	}
	got, ok := pickBestShow("The Prefix at Academy Campus", 0, "anime", hits)
	if !ok || got.ID != 2 {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestPickBestShowPrefixJapaneseListing(t *testing.T) {
	hits := []searchHit{
		{Score: 0.53, Show: show{
			ID: 15, Name: "Romanized Academy Title",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
		{Score: 0.36, Show: show{ID: 9, Name: "The Prefixes", Type: "Scripted"}},
	}
	got, ok := pickBestShow("The Prefix at Academy Campus", 0, "anime", hits)
	if !ok || got.ID != 15 {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestPickBestShowRejectsMainSeriesForSpinoff(t *testing.T) {
	hits := []searchHit{
		{Score: 5.0, Show: show{
			ID: 20, Name: "Main Series Catalog Title!",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
		{Score: 1.0, Show: show{
			ID: 21, Name: "Spinoff Exact Title!",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
	}
	got, ok := pickBestShow("Spinoff Exact Title!", 0, "anime", hits)
	if !ok || got.ID != 21 {
		t.Fatalf("token-matching hit must win over main series, got %#v ok=%v", got, ok)
	}
	filtered := filterExcluded(hits, []string{"20"})
	got, ok = pickBestShow("Spinoff Exact Title!", 0, "anime", filtered)
	if !ok || got.ID != 21 {
		t.Fatalf("after exclude main: got %#v ok=%v", got, ok)
	}
}

func TestPickBestShowSpinoffExact(t *testing.T) {
	hits := []searchHit{
		{Score: 5.0, Show: show{
			ID: 20, Name: "Main Series Catalog Title!",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
		{Score: 1.2, Show: show{
			ID: 21, Name: "Spinoff Exact Title!",
			Type: "Animation", Genres: []string{"Anime"}, Language: "Japanese",
		}},
	}
	got, ok := pickBestShow("Spinoff Exact Title!", 0, "anime", hits)
	if !ok || got.ID != 21 {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestFilterExcluded(t *testing.T) {
	hits := []searchHit{
		{Score: 1, Show: show{ID: 20, Name: "Main"}},
		{Score: 1, Show: show{ID: 9, Name: "Other"}},
	}
	out := filterExcluded(hits, []string{"20"})
	if len(out) != 1 || out[0].Show.ID != 9 {
		t.Fatalf("got %#v", out)
	}
}

func TestPickBestShowYearPreference(t *testing.T) {
	hits := []searchHit{
		{Score: 10, Show: show{ID: 1, Name: "Sample Matter", Premiered: "2024-05-01", Type: "Scripted"}},
		{Score: 9, Show: show{ID: 2, Name: "Sample Matter", Premiered: "2015-06-12", Type: "Scripted"}},
	}
	got, ok := pickBestShow("Sample Matter", 2015, "tv", hits)
	if !ok || got.ID != 2 {
		t.Fatalf("want 2015 show, got %#v ok=%v", got, ok)
	}
}
