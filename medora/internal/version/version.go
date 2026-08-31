package version

import "strings"

var Version string

func Init(v string) {
	Version = strings.TrimSpace(v)
}
