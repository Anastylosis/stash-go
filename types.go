package stash

// Scene is a scene as returned by findScene / findScenes.
type Scene struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Code       string      `json:"code"`
	Date       string      `json:"date"`
	Details    string      `json:"details"`
	Director   string      `json:"director"`
	URLs       []string    `json:"urls"`
	Rating100  *int        `json:"rating100"`
	Organized  bool        `json:"organized"`
	OCounter   int         `json:"o_counter"`
	Files      []File      `json:"files"`
	Tags       []Tag       `json:"tags"`
	Performers []Performer `json:"performers"`
	Studio     *Studio     `json:"studio"`
	StashIDs   []StashID   `json:"stash_ids"`
	Galleries  []Gallery   `json:"galleries"`
	// Captions is populated only when the server's schema has
	// Scene.captions — the scene queries probe for it once and add the
	// field when it is there (see sceneSelection). nil therefore means
	// either "this scene has no captions" or "this server is too old to
	// say"; [Client.Supports] tells the two apart when it matters.
	Captions []Caption `json:"captions"`
}

// HasStashID reports whether the scene carries stash-box metadata.
func (s *Scene) HasStashID() bool { return len(s.StashIDs) > 0 }

// PrimaryFile returns the file Stash treats as canonical, or nil when the
// scene has none.
func (s *Scene) PrimaryFile() *File {
	if len(s.Files) == 0 {
		return nil
	}
	return &s.Files[0]
}

// File is one video file backing a scene. A scene has several when Stash has
// attached re-detected duplicates to it.
type File struct {
	ID           string        `json:"id"`
	Basename     string        `json:"basename"`
	Path         string        `json:"path"`
	Size         int64         `json:"size"`
	ModTime      string        `json:"mod_time"`
	Format       string        `json:"format"`
	Width        int           `json:"width"`
	Height       int           `json:"height"`
	Duration     float64       `json:"duration"`
	VideoCodec   string        `json:"video_codec"`
	AudioCodec   string        `json:"audio_codec"`
	FrameRate    float64       `json:"frame_rate"`
	BitRate      int64         `json:"bit_rate"`
	Fingerprints []Fingerprint `json:"fingerprints"`
}

// Fingerprint returns the value of the named hash ("oshash", "phash", "md5").
func (f *File) Fingerprint(kind string) (string, bool) {
	for _, fp := range f.Fingerprints {
		if fp.Type == kind {
			return fp.Value, true
		}
	}
	return "", false
}

// Fingerprint is one content hash of a file.
// Caption is one subtitle track Stash has attached to a scene. Stash
// discovers these by scanning for sidecar files next to the video; they are
// read-only in GraphQL, so a caption cannot be attached over the API — only
// written to disk and picked up by [Client.MetadataScan].
//
// LanguageCode is the bare ISO 639 subtag Stash parsed off the filename
// (`clip.pt.srt` gives "pt"). Stash parses it with language.ParseBase and
// silently attaches nothing for anything it cannot parse, so a regional tag
// never appears here — a file named `clip.pt-BR.srt` is simply not a
// caption as far as Stash is concerned.
type Caption struct {
	LanguageCode string `json:"language_code"`
	CaptionType  string `json:"caption_type"`
}

type Fingerprint struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Tag attached to a scene.
type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Performer attached to a scene.
// Performer as returned by the performer queries.
//
// A performer reached through a scene carries only ID and Name: the shared
// scene selection asks for nothing more, because a page of scenes would
// otherwise drag a full performer record along for every credit.
// [Client.FindPerformerByID] and [Client.FindPerformers] fill the rest in.
type Performer struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Disambiguation string    `json:"disambiguation"`
	Gender         string    `json:"gender"`
	Birthdate      string    `json:"birthdate"`
	DeathDate      string    `json:"death_date"`
	Country        string    `json:"country"`
	Ethnicity      string    `json:"ethnicity"`
	EyeColor       string    `json:"eye_color"`
	HairColor      string    `json:"hair_color"`
	HeightCM       int       `json:"height_cm"`
	Weight         int       `json:"weight"`
	Measurements   string    `json:"measurements"`
	FakeTits       string    `json:"fake_tits"`
	CareerStart    int       `json:"career_start"`
	CareerEnd      int       `json:"career_end"`
	Tattoos        string    `json:"tattoos"`
	Piercings      string    `json:"piercings"`
	Aliases        []string  `json:"alias_list"`
	URLs           []string  `json:"urls"`
	Details        string    `json:"details"`
	Favorite       bool      `json:"favorite"`
	Rating100      *int      `json:"rating100"`
	ImagePath      string    `json:"image_path"`
	SceneCount     int       `json:"scene_count"`
	Tags           []Tag     `json:"tags"`
	StashIDs       []StashID `json:"stash_ids"`
}

// Studio a scene belongs to.
type Studio struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Gallery attached to a scene.
type Gallery struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// StashID links a scene to its entry in an external stash-box instance.
// Endpoint is the stash-box GraphQL URL; ID is the remote UUID.
type StashID struct {
	Endpoint string `json:"endpoint"`
	ID       string `json:"stash_id"`
}

// SceneUpdate is the payload for a scene update.
//
// Pointer and slice fields are omitted when nil, so only what you set is
// written. That is what makes a partial metadata push non-destructive — an
// unset Title leaves the existing title alone rather than clearing it.
type SceneUpdate struct {
	ID           string    `json:"id"`
	Title        *string   `json:"title,omitempty"`
	Code         *string   `json:"code,omitempty"`
	Details      *string   `json:"details,omitempty"`
	Director     *string   `json:"director,omitempty"`
	Date         *string   `json:"date,omitempty"`
	Rating100    *int      `json:"rating100,omitempty"`
	URLs         []string  `json:"urls,omitempty"`
	TagIDs       []string  `json:"tag_ids,omitempty"`
	PerformerIDs []string  `json:"performer_ids,omitempty"`
	StudioID     *string   `json:"studio_id,omitempty"`
	GalleryIDs   []string  `json:"gallery_ids,omitempty"`
	Organized    *bool     `json:"organized,omitempty"`
	StashIDs     []StashID `json:"stash_ids,omitempty"`

	// CoverImage is a data URI ("data:image/jpeg;base64,…").
	//
	// This package deliberately does not fetch it for you: downloading an
	// arbitrary scraped URL needs SSRF validation, a size cap and an expiry
	// policy, all of which belong to the calling program rather than to a
	// Stash client.
	CoverImage *string `json:"cover_image,omitempty"`
}

// SceneFilter narrows FindScenes.
//
// Fields tagged `json:"-"` are resolved client-side into the server's filter
// shape — PerformerName and StudioName each cost an extra lookup to turn a
// name into an ID.
type SceneFilter struct {
	Organized     *bool  `json:"organized,omitempty"`
	PerformerName string `json:"-"`
	StudioName    string `json:"-"`

	// HasStashID selects scenes that do (true) or do not (false) carry
	// stash-box metadata. Nil means "either".
	HasStashID *bool `json:"-"`

	// PathContains matches scenes whose file path contains this substring.
	PathContains string `json:"-"`

	// HasDate selects scenes that do (true) or do not (false) carry a date.
	// Nil means "either".
	HasDate *bool `json:"-"`

	// TagNames selects scenes carrying every one of these tags, and
	// ExcludeTagNames scenes carrying none of them. Both are resolved to
	// ids first, and a name no tag has is an error rather than an empty
	// result — the same reason [ErrPerformerNotFound] exists.
	TagNames        []string `json:"-"`
	ExcludeTagNames []string `json:"-"`

	// DateBefore and DateAfter bound the date, exclusive at both ends, in
	// Stash's own "2006-01-02" notation. A scene with no date matches
	// neither: an absent date is not an early one.
	DateBefore string `json:"-"`
	DateAfter  string `json:"-"`
}
