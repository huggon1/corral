package app

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStoreUpsertIsIdempotent(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "corral.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	path := t.TempDir()
	first, err := store.UpsertProject(ctx, ProjectSpec{Name: "before", Path: path, Command: []string{"serve", "{port}"}, URLTemplate: "http://localhost:{port}", ReadyURLTemplate: "http://localhost:{port}/ready", Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.UpsertProject(ctx, ProjectSpec{Name: "after", Path: path, Command: []string{"serve", "--port", "{port}"}, URLTemplate: "http://localhost:{port}", ReadyURLTemplate: "http://localhost:{port}/health", Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.Name != "after" {
		t.Fatalf("unexpected projects: %#v %#v", first, second)
	}
	if first.Position != second.Position {
		t.Fatalf("position changed from %d to %d", first.Position, second.Position)
	}
	if second.ReadyURLTemplate != "http://localhost:{port}/health" {
		t.Fatalf("ready URL = %q", second.ReadyURLTemplate)
	}
	projects, err := store.Projects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("got %d projects", len(projects))
	}
}

func TestSQLiteStoreMigratesExistingProjectsToReadyURL(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "corral.db")
	legacy, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	projectPath := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = legacy.Exec(`
		CREATE TABLE projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			command_json TEXT NOT NULL,
			url_template TEXT NOT NULL,
			env_json TEXT NOT NULL DEFAULT '{}',
			last_port INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		INSERT INTO projects (id,name,path,command_json,url_template,env_json,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, ProjectID(projectPath), "legacy", projectPath, `["serve","{port}"]`, "http://localhost:{port}", `{}`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenSQLite(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project, err := store.Project(context.Background(), ProjectID(projectPath))
	if err != nil {
		t.Fatal(err)
	}
	if project.ReadyURLTemplate != project.URLTemplate {
		t.Fatalf("ready URL = %q, URL = %q", project.ReadyURLTemplate, project.URLTemplate)
	}
}

func TestSQLiteStoreRejectsAmbiguousNameLookup(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "corral.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for range 2 {
		_, err := store.UpsertProject(ctx, ProjectSpec{Name: "web", Path: t.TempDir(), Command: []string{"serve", "{port}"}, URLTemplate: "http://localhost:{port}", Env: map[string]string{}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Project(ctx, "web"); err != ErrAmbiguous {
		t.Fatalf("got %v, want %v", err, ErrAmbiguous)
	}
}
