package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/alyshmahell/medora/internal/config"
)

type UserWebhooks struct {
	UserID    int64
	Enabled   bool
	ServerURL string
	APIKey    string
	Destinations []config.WebhookDestination
	UpdatedAt string
}

func (u *UserWebhooks) ToConfig() config.WebhooksConfig {
	if u == nil {
		return config.WebhooksConfig{}
	}
	return config.WebhooksConfig{
		Enabled:      u.Enabled,
		ServerURL:    u.ServerURL,
		Destinations: u.Destinations,
		APIKey:       u.APIKey,
	}
}

func randomWebhookKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (d *DB) GetUserWebhooks(ctx context.Context, userID int64) (*UserWebhooks, error) {
	u := &UserWebhooks{UserID: userID}
	var enabled int
	var destJSON string
	err := d.SQL.QueryRowContext(ctx, `
		SELECT enabled, server_url, api_key, destinations_json, updated_at
		FROM user_webhooks WHERE user_id = ?`, userID).
		Scan(&enabled, &u.ServerURL, &u.APIKey, &destJSON, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		key, err := randomWebhookKey()
		if err != nil {
			return nil, err
		}
		ts := now()
		_, err = d.SQL.ExecContext(ctx, `
			INSERT INTO user_webhooks(user_id, enabled, server_url, api_key, destinations_json, updated_at)
			VALUES (?, 0, '', ?, '[]', ?)`, userID, key, ts)
		if err != nil {
			return nil, err
		}
		u.APIKey = key
		u.Destinations = nil
		u.UpdatedAt = ts
		return u, nil
	}
	if err != nil {
		return nil, err
	}
	u.Enabled = enabled != 0
	if destJSON != "" && destJSON != "[]" {
		if err := json.Unmarshal([]byte(destJSON), &u.Destinations); err != nil {
			return nil, fmt.Errorf("parse destinations: %w", err)
		}
	}
	if u.APIKey == "" {
		key, err := randomWebhookKey()
		if err != nil {
			return nil, err
		}
		ts := now()
		if _, err := d.SQL.ExecContext(ctx,
			`UPDATE user_webhooks SET api_key = ?, updated_at = ? WHERE user_id = ?`,
			key, ts, userID); err != nil {
			return nil, err
		}
		u.APIKey = key
		u.UpdatedAt = ts
	}
	return u, nil
}

func (d *DB) SaveUserWebhooks(ctx context.Context, userID int64, wh config.WebhooksConfig) (*UserWebhooks, error) {
	cur, err := d.GetUserWebhooks(ctx, userID)
	if err != nil {
		return nil, err
	}
	destJSON, err := json.Marshal(wh.Destinations)
	if err != nil {
		return nil, err
	}
	ts := now()
	_, err = d.SQL.ExecContext(ctx, `
		INSERT INTO user_webhooks(user_id, enabled, server_url, api_key, destinations_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			enabled = excluded.enabled,
			server_url = excluded.server_url,
			destinations_json = excluded.destinations_json,
			updated_at = excluded.updated_at`,
		userID, boolInt(wh.Enabled), wh.ServerURL, cur.APIKey, string(destJSON), ts)
	if err != nil {
		return nil, err
	}
	cur.Enabled = wh.Enabled
	cur.ServerURL = wh.ServerURL
	cur.Destinations = wh.Destinations
	cur.UpdatedAt = ts
	return cur, nil
}

func (d *DB) RegenerateUserWebhookKey(ctx context.Context, userID int64) (*UserWebhooks, error) {
	cur, err := d.GetUserWebhooks(ctx, userID)
	if err != nil {
		return nil, err
	}
	key, err := randomWebhookKey()
	if err != nil {
		return nil, err
	}
	ts := now()
	_, err = d.SQL.ExecContext(ctx,
		`UPDATE user_webhooks SET api_key = ?, updated_at = ? WHERE user_id = ?`,
		key, ts, userID)
	if err != nil {
		return nil, err
	}
	cur.APIKey = key
	cur.UpdatedAt = ts
	return cur, nil
}

func (d *DB) ImportLegacyWebhooks(ctx context.Context, userID int64, wh config.WebhooksConfig) error {
	var n int
	if err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_webhooks`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if !wh.Enabled && wh.ServerURL == "" && len(wh.Destinations) == 0 && wh.APIKey == "" {
		return nil
	}
	if _, err := d.GetUserWebhooks(ctx, userID); err != nil {
		return err
	}
	if wh.APIKey != "" {
		if _, err := d.SQL.ExecContext(ctx,
			`UPDATE user_webhooks SET api_key = ? WHERE user_id = ?`, wh.APIKey, userID); err != nil {
			return err
		}
	}
	_, err := d.SaveUserWebhooks(ctx, userID, wh)
	return err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
