package omdb

import "testing"

func TestPickBestSearchHitExactAndYear(t *testing.T) {
	hits := []searchHit{
		{Title: "Five Units Per Hour", Year: "2025", ImdbID: "ttLive"},
		{Title: "Five Units Per Hour", Year: "2007", ImdbID: "ttYearHit"},
		{Title: "Units", Year: "2007", ImdbID: "ttOther"},
	}
	got, ok := pickBestSearchHit("Five Units Per Hour", 2007, hits, "anime")
	if !ok || got.ImdbID != "ttYearHit" {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestPickBestSearchHitWithoutYear(t *testing.T) {
	hits := []searchHit{
		{Title: "Sample Film Reloaded", Year: "2003", ImdbID: "tt2"},
		{Title: "Sample Film", Year: "1999", ImdbID: "tt1"},
	}
	got, ok := pickBestSearchHit("Sample Film", 0, hits, "")
	if !ok || got.ImdbID != "tt1" {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
}

func TestPickBestSearchHitAnimeRanking(t *testing.T) {
	// Search-order first exact when year=0; year pin when year>0.
	hits := []searchHit{
		{Title: "Film Title.", Year: "2016", ImdbID: "ttPrimary"},
		{Title: "Film Title", Year: "2015", ImdbID: "ttOlder"},
		{Title: "Film Title Longer Variant", Year: "2020", ImdbID: "ttLong"},
		{Title: "Film Title.", Year: "2025", ImdbID: "ttRemake"},
	}
	got, ok := pickBestSearchHit("Film Title", 0, hits, "anime")
	if !ok || got.ImdbID != "ttPrimary" {
		t.Fatalf("year=0 search order: got %#v ok=%v", got, ok)
	}
	got, ok = pickBestSearchHit("Film Title", 2016, hits, "anime")
	if !ok || got.ImdbID != "ttPrimary" {
		t.Fatalf("year=2016: got %#v ok=%v", got, ok)
	}
	got, ok = pickBestSearchHit("Film Title", 2015, hits, "anime")
	if !ok || got.ImdbID != "ttOlder" {
		t.Fatalf("year=2015: got %#v ok=%v", got, ok)
	}
	reordered := []searchHit{
		{Title: "Film Title", Year: "2015", ImdbID: "ttOlder"},
		{Title: "Film Title.", Year: "2016", ImdbID: "ttPrimary"},
	}
	got, ok = pickBestSearchHit("Film Title", 0, reordered, "anime")
	if !ok || got.ImdbID != "ttOlder" {
		t.Fatalf("year=0 first hit: got %#v ok=%v", got, ok)
	}
	got, ok = pickBestSearchHit("Film Title", 2016, reordered, "anime")
	if !ok || got.ImdbID != "ttPrimary" {
		t.Fatalf("year=2016 overrides order: got %#v ok=%v", got, ok)
	}
}

func TestPickBestSearchHitAnimeRejectsLongerVariant(t *testing.T) {
	_, ok := pickBestSearchHit("Film Title", 0, []searchHit{
		{Title: "Film Title Longer Variant", Year: "2020", ImdbID: "ttLong"},
	}, "anime")
	if ok {
		t.Fatal("anime must not fuzzy-match longer variant")
	}
}

func TestPickBestSearchHitMovieSuffix(t *testing.T) {
	hits := []searchHit{
		{Title: "Some Film", Year: "2016", ImdbID: "ttFilm"},
		{Title: "Silent Film", Year: "2020", ImdbID: "ttOther"},
	}
	got, ok := pickBestSearchHit("Some Film", 2016, hits, "anime")
	if !ok || got.ImdbID != "ttFilm" {
		t.Fatalf("got %#v ok=%v", got, ok)
	}
	got, ok = pickBestSearchHit("Some Film - The Movie", 2016, hits, "anime")
	if !ok || got.ImdbID != "ttFilm" {
		t.Fatalf("full folder title: got %#v ok=%v", got, ok)
	}
}

func TestAnimeCompatibleHitsPrefersStrippedMovie(t *testing.T) {
	hits := []searchHit{
		{Title: "Some Film", Year: "2016", ImdbID: "ttA"},
		{Title: "Some Film", Year: "2016", ImdbID: "ttB"},
	}
	got := animeCompatibleHits("Some Film - The Movie", 2016, hits)
	if len(got) != 2 {
		t.Fatalf("want both year hits, got %#v", got)
	}
}

func TestTitleKeysCompatibleMovieVariant(t *testing.T) {
	if !titleKeysCompatible("Some Film", "Some Film: The Movie") {
		t.Fatal("prefix+movie")
	}
	if !titleKeysCompatible("Some Film - The Movie", "Some Film") {
		t.Fatal("strip suffix")
	}
}

func TestRuntimeInBallpark(t *testing.T) {
	if !runtimeInBallpark(130, 129) {
		t.Fatal("near")
	}
	if runtimeInBallpark(130, 3) {
		t.Fatal("short vs feature")
	}
}

func TestBetterAnimeMovieCandRuntime(t *testing.T) {
	short := animeMovieCand{anim: false, delta: 127, inBall: false}
	feature := animeMovieCand{anim: true, delta: 1, inBall: true}
	if !betterAnimeMovieCand(feature, short, true) {
		t.Fatal("prefer animation+ballpark")
	}
}
