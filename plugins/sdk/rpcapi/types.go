package rpcapi

// Provider-agnostic RPC types for metadata plugins.

const ServiceName = "Meta"

type StatusArgs struct{}

type StatusReply struct {
	Ready          bool
	DisabledReason string
	Hint           string
}

type ListProvidersArgs struct{}

type ListProvidersReply struct {
	Providers []ProviderInfo
}

type ProviderInfo struct {
	Name string
}

type LookupMovieArgs struct {
	Title           string
	Year            int
	LibraryType     string // movies | tv | anime
	DurationMinutes int
}

type LookupShowArgs struct {
	Title              string
	Year               int
	LibraryType        string
	ExcludeProviderIDs []string
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
	Provider    string
	ProviderID  string
	Message     string
}

type LookupReply struct {
	Result Result
}
