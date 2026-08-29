#!/usr/bin/env python3
"""Minimal OMDb-compatible stub for Medora smoke tests (no real API key / quota)."""
from __future__ import annotations

import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

# 1x1 JPEG
JPEG = bytes([
    0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01,
    0x00, 0x01, 0x00, 0x00, 0xFF, 0xDB, 0x00, 0x43, 0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08,
    0x07, 0x07, 0x07, 0x09, 0x09, 0x08, 0x0A, 0x0C, 0x14, 0x0D, 0x0C, 0x0B, 0x0B, 0x0C, 0x19, 0x12,
    0x13, 0x0F, 0x14, 0x1D, 0x1A, 0x1F, 0x1E, 0x1D, 0x1A, 0x1C, 0x1C, 0x20, 0x24, 0x2E, 0x27, 0x20,
    0x22, 0x2C, 0x23, 0x1C, 0x1C, 0x28, 0x37, 0x29, 0x2C, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1F, 0x27,
    0x39, 0x3D, 0x38, 0x32, 0x3C, 0x2E, 0x33, 0x34, 0x32, 0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x01,
    0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xFF, 0xC4, 0x00, 0x14, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0xFF, 0xC4, 0x00, 0x14,
    0x10, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0xFF, 0xDA, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F, 0x00, 0x7F, 0xFF, 0xD9,
])

HOST = "0.0.0.0"
PORT = 8080
# Reachable from the medora container on the compose network.
POSTER = f"http://omdb-stub:{PORT}/poster.jpg"

FILM_TITLE_2016 = {
    "Title": "Film Title.",
    "Year": "2016",
    "Plot": "Synthetic film for smoke tests.",
    "Runtime": "106 min",
    "imdbRating": "8.4",
    "Poster": POSTER,
    "imdbID": "ttFilm2016",
    "Type": "movie",
    "Response": "True",
}

FILM_TITLE_OTHER = {
    "Title": "Film Title Longer Variant",
    "Year": "2020",
    "Plot": "Wrong film.",
    "Runtime": "114 min",
    "imdbRating": "7.0",
    "Poster": POSTER,
    "imdbID": "ttFilmOther",
    "Type": "movie",
    "Response": "True",
}

BY_ID = {
    "ttFilm2016": FILM_TITLE_2016,
    "ttFilmOther": FILM_TITLE_OTHER,
}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def _json(self, code: int, body: dict):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        parsed = urlparse(self.path)
        if parsed.path.rstrip("/") == "/poster.jpg":
            self.send_response(200)
            self.send_header("Content-Type", "image/jpeg")
            self.send_header("Content-Length", str(len(JPEG)))
            self.end_headers()
            self.wfile.write(JPEG)
            return

        qs = parse_qs(parsed.query)
        path = parsed.path.rstrip("/") or "/"

        # TVMaze / Jikan / TMDB search shapes — empty so synthetic show names stay unmatched.
        if path == "/search/shows" or path.endswith("/search/movie") or path.endswith("/search/tv"):
            self._json(200, [])
            return
        if path == "/anime" or path.endswith("/anime"):
            self._json(200, {"data": []})
            return
        if path.startswith("/3/"):
            self._json(200, {"results": []})
            return

        if "i" in qs:
            imdb = qs["i"][0]
            if imdb in BY_ID:
                self._json(200, BY_ID[imdb])
            else:
                self._json(200, {"Response": "False", "Error": "Incorrect IMDb ID."})
            return

        if "s" in qs:
            title = qs["s"][0].lower()
            if "film title" in title:
                self._json(
                    200,
                    {
                        "Search": [
                            {
                                "Title": "Film Title.",
                                "Year": "2016",
                                "imdbID": "ttFilm2016",
                                "Type": "movie",
                                "Poster": POSTER,
                            },
                            {
                                "Title": "Film Title Longer Variant",
                                "Year": "2020",
                                "imdbID": "ttFilmOther",
                                "Type": "movie",
                                "Poster": POSTER,
                            },
                        ],
                        "totalResults": "2",
                        "Response": "True",
                    },
                )
            else:
                self._json(200, {"Response": "False", "Error": "Movie not found!"})
            return

        if "t" in qs:
            title = qs["t"][0].lower()
            year = (qs.get("y") or [""])[0]
            if "film title" in title and "longer" not in title and "variant" not in title:
                if year == "2015":
                    self._json(200, FILM_TITLE_OTHER)
                else:
                    self._json(200, FILM_TITLE_2016)
                return
            self._json(200, {"Response": "False", "Error": "Movie not found!"})
            return

        self._json(200, {"Response": "False", "Error": "Invalid request"})


def main():
    httpd = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"omdb-stub listening on {HOST}:{PORT}", flush=True)
    httpd.serve_forever()


if __name__ == "__main__":
    main()
