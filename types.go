package stash

// Scene is a scene as returned by findScene / findScenes.
type Scene struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Date       string      `json:"date"`
	Details    string      `json:"details"`
	URLs       []string    `json:"urls"`
	Organized  bool        `json:"organized"`
	Files      []File      `json:"files"`
	Tags       []Tag       `json:"tags"`
	Performers []Performer `json:"performers"`
	Studio     *Studio     `json:"studio"`
	StashIDs   []StashID   `json:"stash_ids"`
}

// File is one video file backing a scene. A scene has several when Stash has
// attached re-detected duplicates to it.
type File struct {
	Basename string  `json:"basename"`
	Path     string  `json:"path"`
	Duration float64 `json:"duration"`
}

// Tag attached to a scene.
type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Performer attached to a scene.
type Performer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Studio a scene belongs to.
type Studio struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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
	ID           string   `json:"id"`
	Title        *string  `json:"title,omitempty"`
	Details      *string  `json:"details,omitempty"`
	Date         *string  `json:"date,omitempty"`
	URLs         []string `json:"urls,omitempty"`
	TagIDs       []string `json:"tag_ids,omitempty"`
	PerformerIDs []string `json:"performer_ids,omitempty"`
	StudioID     *string  `json:"studio_id,omitempty"`
	Organized    *bool    `json:"organized,omitempty"`

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
}
