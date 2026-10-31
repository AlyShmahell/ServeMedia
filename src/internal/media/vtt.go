package media

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// WriteShiftedVTT copies a WebVTT stream to dst with every cue moved
// shiftSeconds earlier. Transcode jobs that resume mid-file replay the media
// timeline from zero, so cues authored against the full file must be moved
// back by that offset to stay in sync with playback.
func WriteShiftedVTT(dst io.Writer, src io.Reader, shiftSeconds float64) error {
	bw := bufio.NewWriter(dst)
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var block []string
	wrote := false
	writeBlock := func() error {
		if len(block) == 0 {
			return nil
		}
		lines, keep := shiftVTTBlock(block, shiftSeconds)
		block = block[:0]
		if !keep {
			return nil
		}
		if wrote {
			if err := bw.WriteByte('\n'); err != nil {
				return err
			}
		}
		for _, line := range lines {
			if _, err := bw.WriteString(line); err != nil {
				return err
			}
			if err := bw.WriteByte('\n'); err != nil {
				return err
			}
		}
		wrote = true
		return nil
	}

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			if err := writeBlock(); err != nil {
				return err
			}
			continue
		}
		block = append(block, line)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if err := writeBlock(); err != nil {
		return err
	}
	return bw.Flush()
}

// shiftVTTBlock retimes one block of WebVTT lines. It reports keep false for
// cues that end at or before the shift point.
func shiftVTTBlock(lines []string, shiftSeconds float64) ([]string, bool) {
	first := strings.TrimSpace(lines[0])
	if strings.HasPrefix(first, "WEBVTT") || strings.HasPrefix(first, "NOTE") ||
		strings.HasPrefix(first, "STYLE") || strings.HasPrefix(first, "REGION") {
		return lines, true
	}
	for i, line := range lines {
		if !strings.Contains(line, "-->") {
			continue
		}
		start, end, settings, ok := parseVTTTiming(line)
		if !ok {
			return lines, true
		}
		start -= shiftSeconds
		end -= shiftSeconds
		if end <= 0 {
			return nil, false
		}
		if start < 0 {
			start = 0
		}
		out := make([]string, 0, len(lines))
		out = append(out, lines[:i]...)
		out = append(out, formatVTTTimestamp(start)+" --> "+formatVTTTimestamp(end)+settings)
		out = append(out, lines[i+1:]...)
		return out, true
	}
	return lines, true
}

func parseVTTTiming(line string) (float64, float64, string, bool) {
	left, right, ok := strings.Cut(line, "-->")
	if !ok {
		return 0, 0, "", false
	}
	start, err := parseVTTTimestamp(left)
	if err != nil {
		return 0, 0, "", false
	}
	fields := strings.Fields(right)
	if len(fields) == 0 {
		return 0, 0, "", false
	}
	end, err := parseVTTTimestamp(fields[0])
	if err != nil {
		return 0, 0, "", false
	}
	settings := ""
	if len(fields) > 1 {
		settings = " " + strings.Join(fields[1:], " ")
	}
	return start, end, settings, true
}

func parseVTTTimestamp(v string) (float64, error) {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(v), ",", "."), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("bad vtt timestamp %q", v)
	}
	sec, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0, fmt.Errorf("bad vtt timestamp %q", v)
	}
	minutes, err := strconv.Atoi(parts[len(parts)-2])
	if err != nil {
		return 0, fmt.Errorf("bad vtt timestamp %q", v)
	}
	total := sec + float64(minutes)*60
	if len(parts) == 3 {
		hours, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, fmt.Errorf("bad vtt timestamp %q", v)
		}
		total += float64(hours) * 3600
	}
	return total, nil
}

func formatVTTTimestamp(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	ms := int64(math.Round(sec * 1000))
	hours := ms / 3600000
	ms -= hours * 3600000
	minutes := ms / 60000
	ms -= minutes * 60000
	seconds := ms / 1000
	ms -= seconds * 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, seconds, ms)
}
