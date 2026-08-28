package stash

// Tier is the resolution class a file belongs to, judged from its pixel
// dimensions. Stash's own `resolution` label is not to be trusted for this:
// libraries carry 720x404 files labelled HD because someone read "720 wide"
// as 720p, and square cover art labelled 8K.
//
// The tiers are bands on the longer side, so 4K is not also 1080-tier and a
// portrait file is judged by its height.
type Tier int

// The bands, on the longer side; each line gives its lower bound.
const (
	TierUnknown Tier = iota // no dimensions, or a side of zero
	TierSD                  // longer side under 1280
	Tier720                 // 1280 up to 1920
	Tier1080                // 1920 up to 2560
	Tier1440                // 2560 up to 3840
	Tier4K                  // 3840 up to 7680
	Tier8K                  // 7680 and beyond
)

var tierNames = [...]string{"unknown", "SD", "720p", "1080p", "1440p", "4K", "8K"}

func (t Tier) String() string {
	if t < 0 || int(t) >= len(tierNames) {
		return "unknown"
	}
	return tierNames[t]
}

// TierOf classifies a frame of the given pixel dimensions. Orientation does
// not matter: the longer side is taken as the width, so 1080x1920 is
// Tier1080 like 1920x1080. Either side at or below zero is TierUnknown.
func TierOf(width, height int) Tier {
	if width <= 0 || height <= 0 {
		return TierUnknown
	}
	long := max(width, height)
	switch {
	case long >= 7680:
		return Tier8K
	case long >= 3840:
		return Tier4K
	case long >= 2560:
		return Tier1440
	case long >= 1920:
		return Tier1080
	case long >= 1280:
		return Tier720
	default:
		return TierSD
	}
}

// Tier classifies the file by its own width and height — see [TierOf].
func (f *File) Tier() Tier {
	return TierOf(f.Width, f.Height)
}
