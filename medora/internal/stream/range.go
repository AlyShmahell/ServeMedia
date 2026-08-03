package stream

import (
	"net/http"
	"os"
)

func ServeFileRange(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "stat failed", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}
