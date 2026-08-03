package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
	CreatedAt    string
}

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

func (d *DB) UserCount(ctx context.Context) (int, error) {
	var n int
	err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return fmt.Sprintf("argon2id$%s$%s", hex.EncodeToString(salt), hex.EncodeToString(hash)), nil
}

func CheckPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	if len(got) != len(want) {
		return false
	}
	var v byte
	for i := range got {
		v |= got[i] ^ want[i]
	}
	return v == 0
}

func (d *DB) CreateUser(ctx context.Context, username, password, role string) (*User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	res, err := d.SQL.ExecContext(ctx,
		`INSERT INTO users(username, password_hash, role, created_at) VALUES (?,?,?,?)`,
		username, hash, role, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &User{ID: id, Username: username, PasswordHash: hash, Role: role, CreatedAt: now()}, nil
}

func (d *DB) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	u := &User{}
	err := d.SQL.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, created_at FROM users WHERE username = ? COLLATE NOCASE`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (d *DB) GetUser(ctx context.Context, id int64) (*User, error) {
	u := &User{}
	err := d.SQL.QueryRowContext(ctx,
		`SELECT id, username, password_hash, role, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (d *DB) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT id, username, password_hash, role, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (d *DB) DeleteUser(ctx context.Context, id int64) error {
	var role string
	var admins int
	if err := d.SQL.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ?`, id).Scan(&role); err != nil {
		return err
	}
	if role == RoleAdmin {
		if err := d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&admins); err != nil {
			return err
		}
		if admins <= 1 {
			return fmt.Errorf("cannot delete last admin")
		}
	}
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func NewSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (d *DB) CreateSession(ctx context.Context, userID int64, days int) (token string, err error) {
	token, err = NewSessionToken()
	if err != nil {
		return "", err
	}
	exp := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
	_, err = d.SQL.ExecContext(ctx,
		`INSERT INTO sessions(user_id, token_hash, expires_at, created_at) VALUES (?,?,?,?)`,
		userID, hashToken(token), exp, now())
	return token, err
}

func (d *DB) UserBySession(ctx context.Context, token string) (*User, error) {
	if token == "" {
		return nil, nil
	}
	u := &User{}
	err := d.SQL.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.password_hash, u.role, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, hashToken(token), now()).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (d *DB) DeleteSession(ctx context.Context, token string) error {
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}
