package main

import (
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/alyshmahell/medora/providers/cascade"
	"github.com/alyshmahell/medora/providers/omdb"
	"github.com/alyshmahell/medora/providers/ratelimit"
	"github.com/alyshmahell/medora/providers/server"
	"github.com/alyshmahell/medora/providers/tvmaze"
)

func main() {
	socket := envOr("MEDORA_PROVIDERS_SOCKET", "/data/run/providers.sock")
	stateDir := envOr("MEDORA_PROVIDERS_STATE", "/data/providers")
	_ = os.MkdirAll(stateDir, 0o755)
	_ = os.MkdirAll(filepath.Dir(socket), 0o755)

	tvRPS := envFloat("MEDORA_TVMAZE_RPS", 2)
	tvDaily := envInt("MEDORA_TVMAZE_DAILY", 0)
	omRPS := envFloat("MEDORA_OMDB_RPS", 1)
	omDaily := envInt("MEDORA_OMDB_DAILY", 1000)

	cas := &cascade.Cascade{
		TVmaze: &tvmaze.Client{
			Limiter: ratelimit.New("tvmaze", tvRPS, tvDaily, stateDir),
		},
		OMDb: &omdb.Client{
			APIKey:  os.Getenv("OMDB_API_KEY"),
			BaseURL: os.Getenv("OMDB_BASE_URL"),
			Limiter: ratelimit.New("omdb", omRPS, omDaily, stateDir),
		},
	}

	log.Printf("medora-providers listening on %s", socket)
	if err := server.ListenAndServe(socket, &server.Service{Cascade: cas}); err != nil {
		log.Fatal(err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envFloat(k string, def float64) float64 {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
