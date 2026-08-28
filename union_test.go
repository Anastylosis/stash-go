package stash

import (
	"reflect"
	"testing"
)

func TestUnionCombinesTagsInOrder(t *testing.T) {
	dst := Scene{ID: "K", Tags: []Tag{{ID: "1"}, {ID: "2"}}}
	src := Scene{ID: "L", Tags: []Tag{{ID: "2"}, {ID: "3"}, {ID: "4"}}}

	u, conflicts := Union(dst, []Scene{src}, DefaultUnionPolicy())
	if len(conflicts) != 0 {
		t.Errorf("conflicts = %+v", conflicts)
	}
	if want := []string{"1", "2", "3", "4"}; !reflect.DeepEqual(u.TagIDs, want) {
		t.Errorf("TagIDs = %v, want %v", u.TagIDs, want)
	}
}

func TestUnionIsZeroWhenNothingChanges(t *testing.T) {
	dst := Scene{
		ID: "K", Title: "the title",
		Tags:       []Tag{{ID: "1"}, {ID: "2"}},
		Performers: []Performer{{ID: "p1"}},
	}
	src := Scene{
		ID: "L", Title: "different",
		Tags:       []Tag{{ID: "1"}},
		Performers: []Performer{{ID: "p1"}},
	}

	u, _ := Union(dst, []Scene{src}, DefaultUnionPolicy())
	if !reflect.DeepEqual(u, SceneUpdate{}) {
		t.Errorf("update = %+v, want zero", u)
	}
}

func TestUnionWithNoSourcesIsZero(t *testing.T) {
	dst := Scene{ID: "K", Tags: []Tag{{ID: "1"}}}

	u, conflicts := Union(dst, nil, DefaultUnionPolicy())
	if !reflect.DeepEqual(u, SceneUpdate{}) || conflicts != nil {
		t.Errorf("update = %+v, conflicts = %v", u, conflicts)
	}
}

func TestUnionFillsEmptyScalarsFromTheFirstSourceThatHasOne(t *testing.T) {
	dst := Scene{ID: "K"}
	srcs := []Scene{
		{ID: "L1", Director: "Steven Spielberg"},
		{ID: "L2", Title: "Real Title", Details: "Real details", Director: "someone else", Studio: &Studio{ID: "s9"}},
	}

	u, _ := Union(dst, srcs, DefaultUnionPolicy())
	if u.Title == nil || *u.Title != "Real Title" {
		t.Errorf("Title = %v", u.Title)
	}
	if u.Details == nil || *u.Details != "Real details" {
		t.Errorf("Details = %v", u.Details)
	}
	if u.Director == nil || *u.Director != "Steven Spielberg" {
		t.Errorf("Director = %v", u.Director)
	}
	if u.StudioID == nil || *u.StudioID != "s9" {
		t.Errorf("StudioID = %v", u.StudioID)
	}
}

func TestUnionFillEmptyKeepsTheDestinationTitle(t *testing.T) {
	dst := Scene{ID: "K", Title: "Keeper title"}
	src := Scene{ID: "L", Title: "Loser title"}

	u, _ := Union(dst, []Scene{src}, DefaultUnionPolicy())
	if u.Title != nil {
		t.Errorf("Title = %q, want untouched", *u.Title)
	}
}

func TestUnionPreferSourceTakesTheSourceTitleButNeverBlanksIt(t *testing.T) {
	p := UnionPolicy{Title: PreferSource}
	dst := Scene{ID: "K", Title: "Keeper title"}

	u, _ := Union(dst, []Scene{{ID: "L", Title: "Loser title"}}, p)
	if u.Title == nil || *u.Title != "Loser title" {
		t.Errorf("Title = %v, want the source's", u.Title)
	}
	u, _ = Union(dst, []Scene{{ID: "L"}}, p)
	if u.Title != nil {
		t.Errorf("Title = %q, want untouched by an empty source", *u.Title)
	}
}

func TestUnionKeepLeavesEverything(t *testing.T) {
	dst := Scene{ID: "K"}
	src := Scene{ID: "L", Title: "x", Tags: []Tag{{ID: "1"}}, Organized: true}

	u, _ := Union(dst, []Scene{src}, UnionPolicy{})
	if !reflect.DeepEqual(u, SceneUpdate{}) {
		t.Errorf("update = %+v, want zero", u)
	}
}

func TestUnionRatingTakesTheHighest(t *testing.T) {
	r70, r90 := 70, 90
	u, _ := Union(Scene{Rating100: &r70}, []Scene{{Rating100: &r90}}, DefaultUnionPolicy())
	if u.Rating100 == nil || *u.Rating100 != 90 {
		t.Errorf("Rating100 = %v, want 90", u.Rating100)
	}
	u, _ = Union(Scene{Rating100: &r90}, []Scene{{Rating100: &r70}}, DefaultUnionPolicy())
	if u.Rating100 != nil {
		t.Errorf("Rating100 = %d, want untouched", *u.Rating100)
	}
}

func TestUnionOrganizedIfAnyCopyWas(t *testing.T) {
	u, _ := Union(Scene{}, []Scene{{}, {Organized: true}}, DefaultUnionPolicy())
	if u.Organized == nil || !*u.Organized {
		t.Errorf("Organized = %v, want true", u.Organized)
	}
	u, _ = Union(Scene{Organized: true}, []Scene{{}}, DefaultUnionPolicy())
	if u.Organized != nil {
		t.Errorf("Organized = %v, want untouched", *u.Organized)
	}
}

func TestUnionStashIDsDeduplicateAcrossEndpoints(t *testing.T) {
	dst := Scene{StashIDs: []StashID{{Endpoint: "https://stashdb.org", ID: "abc"}}}
	src := Scene{StashIDs: []StashID{
		{Endpoint: "https://stashdb.org", ID: "abc"},
		{Endpoint: "https://fansdb.cc", ID: "xyz"},
	}}

	u, conflicts := Union(dst, []Scene{src}, DefaultUnionPolicy())
	want := []StashID{{Endpoint: "https://stashdb.org", ID: "abc"}, {Endpoint: "https://fansdb.cc", ID: "xyz"}}
	if !reflect.DeepEqual(u.StashIDs, want) {
		t.Errorf("StashIDs = %v, want %v", u.StashIDs, want)
	}
	if len(conflicts) != 0 {
		t.Errorf("conflicts = %+v", conflicts)
	}
}

func TestUnionReportsAConflictingStashIDAndKeepsTheDestinations(t *testing.T) {
	dst := Scene{StashIDs: []StashID{{Endpoint: "https://stashdb.org", ID: "abc"}}}
	srcs := []Scene{
		{StashIDs: []StashID{{Endpoint: "https://stashdb.org", ID: "def"}}},
		{StashIDs: []StashID{{Endpoint: "https://fansdb.cc", ID: "x1"}}},
		{StashIDs: []StashID{{Endpoint: "https://fansdb.cc", ID: "x2"}}},
	}

	u, conflicts := Union(dst, srcs, DefaultUnionPolicy())
	want := []StashID{{Endpoint: "https://stashdb.org", ID: "abc"}, {Endpoint: "https://fansdb.cc", ID: "x1"}}
	if !reflect.DeepEqual(u.StashIDs, want) {
		t.Errorf("StashIDs = %v, want %v", u.StashIDs, want)
	}
	wantConflicts := []Conflict{
		{Endpoint: "https://stashdb.org", Kept: "abc", Dropped: "def"},
		{Endpoint: "https://fansdb.cc", Kept: "x1", Dropped: "x2"},
	}
	if !reflect.DeepEqual(conflicts, wantConflicts) {
		t.Errorf("conflicts = %+v, want %+v", conflicts, wantConflicts)
	}
}

func TestUnionConflictAloneLeavesStashIDsUntouched(t *testing.T) {
	dst := Scene{StashIDs: []StashID{{Endpoint: "https://stashdb.org", ID: "abc"}}}
	src := Scene{StashIDs: []StashID{{Endpoint: "https://stashdb.org", ID: "def"}}}

	u, conflicts := Union(dst, []Scene{src}, DefaultUnionPolicy())
	if u.StashIDs != nil {
		t.Errorf("StashIDs = %v, want untouched", u.StashIDs)
	}
	if len(conflicts) != 1 {
		t.Errorf("conflicts = %+v, want one", conflicts)
	}
}

func TestUnionCombinesURLsAndPerformersAndGalleries(t *testing.T) {
	dst := Scene{URLs: []string{"http://a"}, Performers: []Performer{{ID: "p1"}}, Galleries: []Gallery{{ID: "g1"}}}
	src := Scene{URLs: []string{"http://a", "http://b"}, Performers: []Performer{{ID: "p2"}}, Galleries: []Gallery{{ID: "g1"}}}

	u, _ := Union(dst, []Scene{src}, DefaultUnionPolicy())
	if want := []string{"http://a", "http://b"}; !reflect.DeepEqual(u.URLs, want) {
		t.Errorf("URLs = %v, want %v", u.URLs, want)
	}
	if want := []string{"p1", "p2"}; !reflect.DeepEqual(u.PerformerIDs, want) {
		t.Errorf("PerformerIDs = %v, want %v", u.PerformerIDs, want)
	}
	if u.GalleryIDs != nil {
		t.Errorf("GalleryIDs = %v, want untouched", u.GalleryIDs)
	}
}

func TestUnionDoesNotModifyItsArguments(t *testing.T) {
	dst := Scene{Tags: []Tag{{ID: "1"}}, StashIDs: make([]StashID, 1, 4)}
	dst.StashIDs[0] = StashID{Endpoint: "e", ID: "1"}
	src := Scene{Tags: []Tag{{ID: "2"}}, StashIDs: []StashID{{Endpoint: "f", ID: "2"}}}

	u, _ := Union(dst, []Scene{src}, DefaultUnionPolicy())
	u.StashIDs[0].ID = "changed"
	if dst.StashIDs[0].ID != "1" || len(dst.Tags) != 1 {
		t.Errorf("dst was modified: %+v", dst)
	}
}
