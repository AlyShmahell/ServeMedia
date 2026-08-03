package rpcapi

// Provider-agnostic RPC types. Concrete provider names may appear only in
// string fields filled by the medora-providers sidecar at runtime.

const ServiceName = "Meta"

type StatusArgs struct{}

type StatusReply struct {
	Ready          bool
	DisabledReason string
	Hint           string
}

type LookupMovieArgs struct {
	Title            string
	Year             int
	LibraryType      string // movies | tv | anime
	DurationMinutes  int    // optional ffprobe duration; 0 = unknown
}

type LookupShowArgs struct {
	Title              string
	Year               int // optional folder year; 0 = unknown
	LibraryType        string
	ExcludeProviderIDs []string // skip these provider ids (e.g. already used in library)
}

type LookupSeasonArgs struct {
	ShowTitle      string
	Season         int
	LibraryType    string
	ShowProvider   string
	ShowProviderID string
}

type LookupEpisodeArgs struct {
	ShowTitle      string
	Season         int
	Episode        int
	LibraryType    string
	ShowProvider   string
	ShowProviderID string
}

// Result is a provider-agnostic metadata payload.
type Result struct {
	Title       string
	Year        int
	Plot        string
	Tagline     string
	Runtime     int
	Rating      float64
	PosterURL   string
	BackdropURL string
	StillURL    string
	Provider    string // opaque
	ProviderID  string // opaque
	Message     string // optional UI/progress text from sidecar
}

type LookupReply struct {
	Result Result
}
