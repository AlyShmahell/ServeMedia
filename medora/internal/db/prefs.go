package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

const (
	PrefsScopeUser    = "user"
	PrefsScopeLibrary = "library"
	PrefsScopeShow    = "show"
	PrefsScopeMovie   = "movie"
)

// PrefsPatch holds fields to write; nil pointer means leave unchanged.
type PrefsPatch struct {
	Volume       *float64
	Muted        *bool
	Height       *int
	AudioLang    *string
	SubtitleLang *string // non-nil "" means Off
}

// ResolvedPrefs is the cascade result (user ← library ← show/movie).
type ResolvedPrefs struct {
	Volume       float64
	Muted        bool
	HasVolume    bool
	HasMuted     bool
	Height       *int    // nil = unset
	AudioLang    string  // "" = unset
	SubtitleLang *string // nil = unset; non-nil "" = Off
}

type prefsRow struct {
	Volume       sql.NullFloat64
	Muted        sql.NullInt64
	Height       sql.NullInt64
	AudioLang    sql.NullString
	SubtitleLang sql.NullString
}

// PlaybackScopeIDs resolves library + content scope for prefs.
type PlaybackScopeIDs struct {
	LibraryID int64
	// ItemScope is show|movie; ItemID is show media_item id or movie id.
	ItemScope string
	ItemID    int64
}

func (d *DB) PlaybackScopeForMovie(ctx context.Context, movieID int64) (*PlaybackScopeIDs, error) {
	it, err := d.GetMediaItem(ctx, movieID)
	if err != nil || it == nil {
		return nil, err
	}
	return &PlaybackScopeIDs{LibraryID: it.LibraryID, ItemScope: PrefsScopeMovie, ItemID: it.ID}, nil
}

func (d *DB) PlaybackScopeForEpisode(ctx context.Context, episodeID int64) (*PlaybackScopeIDs, error) {
	ep, err := d.GetEpisode(ctx, episodeID)
	if err != nil || ep == nil {
		return nil, err
	}
	it, err := d.GetMediaItem(ctx, ep.ShowID)
	if err != nil || it == nil {
		return nil, err
	}
	return &PlaybackScopeIDs{LibraryID: it.LibraryID, ItemScope: PrefsScopeShow, ItemID: ep.ShowID}, nil
}

func (d *DB) getPrefsRow(ctx context.Context, userID int64, scope string, scopeID int64) (prefsRow, error) {
	var row prefsRow
	err := d.SQL.QueryRowContext(ctx, `
		SELECT volume, muted, height, audio_lang, subtitle_lang
		FROM playback_prefs WHERE user_id=? AND scope=? AND scope_id=?`,
		userID, scope, scopeID).
		Scan(&row.Volume, &row.Muted, &row.Height, &row.AudioLang, &row.SubtitleLang)
	if errors.Is(err, sql.ErrNoRows) {
		return prefsRow{}, nil
	}
	return row, err
}

func applyRow(dst *ResolvedPrefs, row prefsRow) {
	if row.Volume.Valid {
		dst.Volume = row.Volume.Float64
		dst.HasVolume = true
	}
	if row.Muted.Valid {
		dst.Muted = row.Muted.Int64 != 0
		dst.HasMuted = true
	}
	if row.Height.Valid {
		h := int(row.Height.Int64)
		dst.Height = &h
	}
	if row.AudioLang.Valid {
		dst.AudioLang = row.AudioLang.String
	}
	if row.SubtitleLang.Valid {
		s := row.SubtitleLang.String
		dst.SubtitleLang = &s
	}
}

// ResolvePlaybackPrefs merges user → library → show/movie (later wins).
func (d *DB) ResolvePlaybackPrefs(ctx context.Context, userID int64, scope *PlaybackScopeIDs) (ResolvedPrefs, error) {
	out := ResolvedPrefs{Volume: 1}
	if scope == nil {
		row, err := d.getPrefsRow(ctx, userID, PrefsScopeUser, 0)
		if err != nil {
			return out, err
		}
		applyRow(&out, row)
		return out, nil
	}
	userRow, err := d.getPrefsRow(ctx, userID, PrefsScopeUser, 0)
	if err != nil {
		return out, err
	}
	applyRow(&out, userRow)
	if scope.LibraryID > 0 {
		libRow, err := d.getPrefsRow(ctx, userID, PrefsScopeLibrary, scope.LibraryID)
		if err != nil {
			return out, err
		}
		applyRow(&out, libRow)
	}
	if scope.ItemID > 0 && (scope.ItemScope == PrefsScopeShow || scope.ItemScope == PrefsScopeMovie) {
		itemRow, err := d.getPrefsRow(ctx, userID, scope.ItemScope, scope.ItemID)
		if err != nil {
			return out, err
		}
		applyRow(&out, itemRow)
	}
	return out, nil
}

func (d *DB) writePrefsRow(ctx context.Context, userID int64, scope string, scopeID int64, row prefsRow) error {
	_, err := d.SQL.ExecContext(ctx, `
		INSERT INTO playback_prefs(user_id, scope, scope_id, volume, muted, height, audio_lang, subtitle_lang, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id, scope, scope_id) DO UPDATE SET
			volume=excluded.volume,
			muted=excluded.muted,
			height=excluded.height,
			audio_lang=excluded.audio_lang,
			subtitle_lang=excluded.subtitle_lang,
			updated_at=excluded.updated_at`,
		userID, scope, scopeID,
		nullFloat64(row.Volume), nullInt64(row.Muted), nullInt64(row.Height),
		nullString(row.AudioLang), nullString(row.SubtitleLang), now())
	return err
}

func nullFloat64(v sql.NullFloat64) any {
	if !v.Valid {
		return nil
	}
	return v.Float64
}

func nullInt64(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func nullString(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}

func mergePatch(row *prefsRow, patch PrefsPatch) {
	if patch.Volume != nil {
		row.Volume = sql.NullFloat64{Float64: *patch.Volume, Valid: true}
	}
	if patch.Muted != nil {
		m := int64(0)
		if *patch.Muted {
			m = 1
		}
		row.Muted = sql.NullInt64{Int64: m, Valid: true}
	}
	if patch.Height != nil {
		row.Height = sql.NullInt64{Int64: int64(*patch.Height), Valid: true}
	}
	if patch.AudioLang != nil {
		row.AudioLang = sql.NullString{String: strings.TrimSpace(*patch.AudioLang), Valid: true}
	}
	if patch.SubtitleLang != nil {
		row.SubtitleLang = sql.NullString{String: strings.TrimSpace(*patch.SubtitleLang), Valid: true}
	}
}

// SavePlaybackPrefs writes the patch to user + library + item scopes when IDs are set.
func (d *DB) SavePlaybackPrefs(ctx context.Context, userID int64, scope *PlaybackScopeIDs, patch PrefsPatch) error {
	targets := []struct {
		scope string
		id    int64
	}{{PrefsScopeUser, 0}}
	if scope != nil {
		if scope.LibraryID > 0 {
			targets = append(targets, struct {
				scope string
				id    int64
			}{PrefsScopeLibrary, scope.LibraryID})
		}
		if scope.ItemID > 0 && (scope.ItemScope == PrefsScopeShow || scope.ItemScope == PrefsScopeMovie) {
			targets = append(targets, struct {
				scope string
				id    int64
			}{scope.ItemScope, scope.ItemID})
		}
	}
	for _, t := range targets {
		row, err := d.getPrefsRow(ctx, userID, t.scope, t.id)
		if err != nil {
			return err
		}
		mergePatch(&row, patch)
		if err := d.writePrefsRow(ctx, userID, t.scope, t.id, row); err != nil {
			return err
		}
	}
	return nil
}

