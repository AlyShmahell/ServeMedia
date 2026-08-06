package watchdog

import (
	"net/http/httputil"
	"net/url"
)

func newReverseProxy(target string) *httputil.ReverseProxy {
	u, err := url.Parse(target)
	if err != nil {
		panic("watchdog: invalid proxy URL: " + err.Error())
	}
	return httputil.NewSingleHostReverseProxy(u)
}
