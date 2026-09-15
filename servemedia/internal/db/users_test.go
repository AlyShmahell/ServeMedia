package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alyshmahell/medora/internal/db"
)

func TestRegisterOnceAndSession(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	n, _ := d.UserCount(ctx)
	if n != 0 {
		t.Fatal("expected empty")
	}
	u, err := d.CreateUser(ctx, "admin", "secret", db.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != db.RoleAdmin {
		t.Fatal(u.Role)
	}
	if !db.CheckPassword(u.PasswordHash, "secret") {
		t.Fatal("password check")
	}
	tok, err := d.CreateSession(ctx, u.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.UserBySession(ctx, tok)
	if err != nil || got == nil || got.Username != "admin" {
		t.Fatalf("session user %#v %v", got, err)
	}
}

func TestUpdateUserPassword(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, err := d.CreateUser(ctx, "alice", "oldpass", db.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateUserPassword(ctx, u.ID, "newpass"); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetUser(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if db.CheckPassword(got.PasswordHash, "newpass") != true {
		t.Fatal("expected new password to work")
	}
	if db.CheckPassword(got.PasswordHash, "oldpass") {
		t.Fatal("old password should no longer work")
	}
}

func TestCannotDeleteLastAdmin(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	u, _ := d.CreateUser(ctx, "admin", "x", db.RoleAdmin)
	if err := d.DeleteUser(ctx, u.ID); err == nil {
		t.Fatal("expected error")
	}
}
