package watchdog

import (
	"bytes"
	"embed"
	"html/template"
	"io"
	"sync"
	"time"
)

//go:embed wake.html wake.css wake.svg
var wakeAssets embed.FS

const minWakePoll = 200 * time.Millisecond

type wakePageData struct {
	WakePollMs int
	PulseMs    int
	CSS        template.CSS
	SVG        template.HTML
}

var (
	wakeTmpl     *template.Template
	wakeTmplOnce sync.Once
	wakeTmplErr  error
)

func loadWakeTemplate() (*template.Template, error) {
	wakeTmplOnce.Do(func() {
		wakeTmpl, wakeTmplErr = template.ParseFS(wakeAssets, "wake.html")
	})
	return wakeTmpl, wakeTmplErr
}

func wakePollInterval(cfg Config) time.Duration {
	d := cfg.WakePoll
	if d <= 0 {
		d = 750 * time.Millisecond
	}
	if d < minWakePoll {
		d = minWakePoll
	}
	return d
}

func renderWakePage(w io.Writer, cfg Config) error {
	tmpl, err := loadWakeTemplate()
	if err != nil {
		return err
	}
	css, err := wakeAssets.ReadFile("wake.css")
	if err != nil {
		return err
	}
	svg, err := wakeAssets.ReadFile("wake.svg")
	if err != nil {
		return err
	}
	data := wakePageData{
		WakePollMs: int(wakePollInterval(cfg) / time.Millisecond),
		PulseMs:    int(cfg.PulseHint / time.Millisecond),
		CSS:        template.CSS(css),
		SVG:        template.HTML(svg),
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}
	_, err = w.Write(buf.Bytes())
	return err
}
