package stash

// FieldPolicy says how Union combines one scene field.
//
// The zero value is Keep, so a field left out of a UnionPolicy is never
// written.
type FieldPolicy int

const (
	// Keep never touches the field.
	Keep FieldPolicy = iota

	// FillEmpty keeps the destination's value unless it is empty, and then
	// takes the first source that has one. This is what a merge wants: the
	// scene being kept is right, the ones being folded away fill its gaps.
	FillEmpty

	// PreferSource takes the first source that has a value, over whatever the
	// destination has. An empty source never clears the destination.
	PreferSource

	// Combine takes everything: list fields get every element from every
	// scene, the destination's first, deduplicated; Rating100 takes the
	// highest; Organized is true if any scene is. On a plain string field it
	// behaves as FillEmpty, there being nothing to combine.
	Combine
)

// UnionPolicy is a FieldPolicy per scene field Union knows how to combine.
// Fields Stash's sceneMerge already handles — files, markers, play history —
// are not here.
type UnionPolicy struct {
	Title    FieldPolicy
	Code     FieldPolicy
	Date     FieldPolicy
	Details  FieldPolicy
	Director FieldPolicy
	Studio   FieldPolicy

	URLs       FieldPolicy
	Tags       FieldPolicy
	Performers FieldPolicy
	Galleries  FieldPolicy
	StashIDs   FieldPolicy

	Rating100 FieldPolicy
	Organized FieldPolicy
}

// DefaultUnionPolicy is the policy a merge wants: lists are unioned, scalars
// are kept unless the destination has none, the rating is the highest and
// the scene is organized if any copy was.
func DefaultUnionPolicy() UnionPolicy {
	return UnionPolicy{
		Title:      FillEmpty,
		Code:       FillEmpty,
		Date:       FillEmpty,
		Details:    FillEmpty,
		Director:   FillEmpty,
		Studio:     FillEmpty,
		URLs:       Combine,
		Tags:       Combine,
		Performers: Combine,
		Galleries:  Combine,
		StashIDs:   Combine,
		Rating100:  Combine,
		Organized:  Combine,
	}
}

// Conflict is a stash ID Union dropped because the scene already had a
// different one for the same endpoint. A scene holds one ID per stash-box,
// so two IDs for one endpoint means two different remote scenes claim the
// same local one — which is for a person to settle, not this function.
type Conflict struct {
	Endpoint string
	Kept     string
	Dropped  string
}

// Union computes the values a merge into dst needs so that nothing the
// sources know is lost, since sceneMerge on its own keeps only dst's
// metadata. The result is partial: only fields that would change are set,
// and it is the zero SceneUpdate when nothing would. Its ID is left empty;
// [Client.MergeScenes] addresses it at the destination.
//
// Lists deduplicate by ID, stash IDs by (endpoint, id), and dst's order comes
// first. When the scenes disagree on the ID for one endpoint, dst wins over a
// source and an earlier source over a later one; every ID dropped that way is
// returned as a Conflict.
//
// The function is pure: it neither reads nor writes Stash, and does not
// modify its arguments.
func Union(dst Scene, srcs []Scene, p UnionPolicy) (SceneUpdate, []Conflict) {
	var u SceneUpdate
	if len(srcs) == 0 {
		return u, nil
	}

	u.Title = unionString(p.Title, dst.Title, srcs, func(s Scene) string { return s.Title })
	u.Code = unionString(p.Code, dst.Code, srcs, func(s Scene) string { return s.Code })
	u.Date = unionString(p.Date, dst.Date, srcs, func(s Scene) string { return s.Date })
	u.Details = unionString(p.Details, dst.Details, srcs, func(s Scene) string { return s.Details })
	u.Director = unionString(p.Director, dst.Director, srcs, func(s Scene) string { return s.Director })
	u.StudioID = unionString(p.Studio, studioID(dst), srcs, studioID)

	u.URLs = unionList(p.URLs, dst.URLs, srcs, func(s Scene) []string { return s.URLs })
	u.TagIDs = unionList(p.Tags, tagIDs(dst), srcs, tagIDs)
	u.PerformerIDs = unionList(p.Performers, performerIDs(dst), srcs, performerIDs)
	u.GalleryIDs = unionList(p.Galleries, galleryIDs(dst), srcs, galleryIDs)

	var conflicts []Conflict
	u.StashIDs, conflicts = unionStashIDs(p.StashIDs, dst.StashIDs, srcs)

	u.Rating100 = unionRating(p.Rating100, dst.Rating100, srcs)
	u.Organized = unionOrganized(p.Organized, dst.Organized, srcs)
	return u, conflicts
}

// unionString returns the new value for a string field, or nil to leave it.
func unionString(p FieldPolicy, have string, srcs []Scene, of func(Scene) string) *string {
	switch p {
	case FillEmpty, Combine:
		if have != "" {
			return nil
		}
	case PreferSource:
	default:
		return nil
	}
	for _, s := range srcs {
		v := of(s)
		if v == "" {
			continue
		}
		if v == have {
			return nil
		}
		return &v
	}
	return nil
}

// unionList returns the new value for a list field, or nil to leave it.
// Empty elements are dropped, since an empty ID is never one Stash issued.
func unionList(p FieldPolicy, have []string, srcs []Scene, of func(Scene) []string) []string {
	var base []string
	switch p {
	case Combine:
		base = have
	case FillEmpty:
		if len(have) > 0 {
			return nil
		}
	case PreferSource:
	default:
		return nil
	}
	seen := make(map[string]bool, len(base))
	out := make([]string, 0, len(base))
	for _, v := range base {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	added := false
	for _, s := range srcs {
		for _, v := range of(s) {
			if v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
				added = true
			}
		}
	}
	if !added {
		return nil
	}
	if p == PreferSource && sameStrings(out, have) {
		return nil
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// unionStashIDs is unionList for stash IDs, keyed on (endpoint, id) and
// holding one ID per endpoint.
func unionStashIDs(p FieldPolicy, have []StashID, srcs []Scene) ([]StashID, []Conflict) {
	var base []StashID
	switch p {
	case Combine:
		base = have
	case FillEmpty:
		if len(have) > 0 {
			return nil, nil
		}
	case PreferSource:
	default:
		return nil, nil
	}
	byEndpoint := make(map[string]string, len(base))
	out := make([]StashID, 0, len(base))
	for _, id := range base {
		if _, ok := byEndpoint[id.Endpoint]; !ok {
			byEndpoint[id.Endpoint] = id.ID
			out = append(out, id)
		}
	}
	var conflicts []Conflict
	added := false
	for _, s := range srcs {
		for _, id := range s.StashIDs {
			kept, ok := byEndpoint[id.Endpoint]
			switch {
			case !ok:
				byEndpoint[id.Endpoint] = id.ID
				out = append(out, id)
				added = true
			case kept != id.ID:
				conflicts = append(conflicts, Conflict{Endpoint: id.Endpoint, Kept: kept, Dropped: id.ID})
			}
		}
	}
	if !added {
		return nil, conflicts
	}
	return out, conflicts
}

func unionRating(p FieldPolicy, have *int, srcs []Scene) *int {
	switch p {
	case Combine:
		best := have
		for _, s := range srcs {
			if s.Rating100 != nil && (best == nil || *s.Rating100 > *best) {
				best = s.Rating100
			}
		}
		if best == have || (have != nil && *best == *have) {
			return nil
		}
		v := *best
		return &v
	case FillEmpty:
		if have != nil {
			return nil
		}
		fallthrough
	case PreferSource:
		for _, s := range srcs {
			if s.Rating100 != nil {
				if have != nil && *have == *s.Rating100 {
					return nil
				}
				v := *s.Rating100
				return &v
			}
		}
	}
	return nil
}

func unionOrganized(p FieldPolicy, have bool, srcs []Scene) *bool {
	switch p {
	case Combine, FillEmpty:
		if have {
			return nil
		}
		for _, s := range srcs {
			if s.Organized {
				t := true
				return &t
			}
		}
	case PreferSource:
		if v := srcs[0].Organized; v != have {
			return &v
		}
	}
	return nil
}

func studioID(s Scene) string {
	if s.Studio == nil {
		return ""
	}
	return s.Studio.ID
}

func tagIDs(s Scene) []string {
	ids := make([]string, 0, len(s.Tags))
	for _, t := range s.Tags {
		ids = append(ids, t.ID)
	}
	return ids
}

func performerIDs(s Scene) []string {
	ids := make([]string, 0, len(s.Performers))
	for _, p := range s.Performers {
		ids = append(ids, p.ID)
	}
	return ids
}

func galleryIDs(s Scene) []string {
	ids := make([]string, 0, len(s.Galleries))
	for _, g := range s.Galleries {
		ids = append(ids, g.ID)
	}
	return ids
}
