package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound  = errors.New("project not found")
	ErrAmbiguous = errors.New("project name is ambiguous; use its ID")
)

type Store interface {
	UpsertProject(context.Context, ProjectSpec) (Project, error)
	Projects(context.Context) ([]Project, error)
	Project(context.Context, string) (Project, error)
	RemoveProject(context.Context, string) error
	SaveRun(context.Context, Run) error
	Run(context.Context, string) (*Run, error)
	ClearRun(context.Context, string) error
	SetLastPort(context.Context, string, int) error
	MoveProject(context.Context, string, int) (bool, error)
	Close() error
}

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open registry: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) migrate() error {
	_, err := s.db.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA busy_timeout = 5000;
		CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			path TEXT NOT NULL UNIQUE,
			command_json TEXT NOT NULL,
			url_template TEXT NOT NULL,
			ready_url_template TEXT NOT NULL DEFAULT '',
			env_json TEXT NOT NULL DEFAULT '{}',
			last_port INTEGER NOT NULL DEFAULT 0,
			position INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS runs (
			project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
			pid INTEGER NOT NULL,
			port INTEGER NOT NULL,
			log_path TEXT NOT NULL,
			started_at TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("migrate registry: %w", err)
	}
	hasReadyURL, err := s.projectColumnExists("ready_url_template")
	if err != nil {
		return fmt.Errorf("inspect registry schema: %w", err)
	}
	if !hasReadyURL {
		if _, err := s.db.Exec(`ALTER TABLE projects ADD COLUMN ready_url_template TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add ready URL column: %w", err)
		}
	}
	hasPosition, err := s.projectColumnExists("position")
	if err != nil {
		return fmt.Errorf("inspect registry schema: %w", err)
	}
	if !hasPosition {
		if _, err := s.db.Exec(`ALTER TABLE projects ADD COLUMN position INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add project position column: %w", err)
		}
	}
	if err := s.normalizeProjectPositions(); err != nil {
		return fmt.Errorf("normalize project positions: %w", err)
	}
	return nil
}

func (s *SQLiteStore) projectColumnExists(target string) (bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(projects)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == target {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *SQLiteStore) normalizeProjectPositions() error {
	rows, err := s.db.Query(`SELECT id FROM projects ORDER BY position ASC, name COLLATE NOCASE ASC, id ASC`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for position, id := range ids {
		if _, err := tx.Exec(`UPDATE projects SET position=? WHERE id=?`, position, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) UpsertProject(ctx context.Context, spec ProjectSpec) (Project, error) {
	now := time.Now().UTC()
	id := ProjectID(spec.Path)
	command, _ := json.Marshal(spec.Command)
	env, _ := json.Marshal(spec.Env)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO projects (id,name,path,command_json,url_template,ready_url_template,env_json,position,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,(SELECT COALESCE(MAX(position),-1)+1 FROM projects),?,?)
		ON CONFLICT(path) DO UPDATE SET
			name=excluded.name, command_json=excluded.command_json,
			url_template=excluded.url_template, ready_url_template=excluded.ready_url_template,
			env_json=excluded.env_json,
			updated_at=excluded.updated_at`, id, spec.Name, spec.Path, command,
		spec.URLTemplate, spec.ReadyURLTemplate, env, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Project{}, fmt.Errorf("upsert project: %w", err)
	}
	return s.Project(ctx, id)
}

func (s *SQLiteStore) Projects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,path,command_json,url_template,ready_url_template,env_json,last_port,position,created_at,updated_at FROM projects ORDER BY position ASC, name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	var projects []Project
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanProject(row scanner) (Project, error) {
	var project Project
	var command, env, created, updated string
	if err := row.Scan(&project.ID, &project.Name, &project.Path, &command, &project.URLTemplate, &project.ReadyURLTemplate, &env, &project.LastPort, &project.Position, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return project, ErrNotFound
		}
		return project, fmt.Errorf("scan project: %w", err)
	}
	if err := json.Unmarshal([]byte(command), &project.Command); err != nil {
		return project, fmt.Errorf("decode command: %w", err)
	}
	if err := json.Unmarshal([]byte(env), &project.Env); err != nil {
		return project, fmt.Errorf("decode environment: %w", err)
	}
	if project.ReadyURLTemplate == "" {
		project.ReadyURLTemplate = project.URLTemplate
	}
	project.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	project.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return project, nil
}

func (s *SQLiteStore) Project(ctx context.Context, id string) (Project, error) {
	query := `SELECT id,name,path,command_json,url_template,ready_url_template,env_json,last_port,position,created_at,updated_at FROM projects WHERE id=?`
	project, err := scanProject(s.db.QueryRowContext(ctx, query, id))
	if err == nil || !errors.Is(err, ErrNotFound) {
		return project, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,path,command_json,url_template,ready_url_template,env_json,last_port,position,created_at,updated_at FROM projects WHERE name=? LIMIT 2`, id)
	if err != nil {
		return Project{}, err
	}
	defer rows.Close()
	var matches []Project
	for rows.Next() {
		match, err := scanProject(rows)
		if err != nil {
			return Project{}, err
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return Project{}, err
	}
	switch len(matches) {
	case 0:
		return Project{}, ErrNotFound
	case 1:
		return matches[0], nil
	default:
		return Project{}, ErrAmbiguous
	}
}

func (s *SQLiteStore) RemoveProject(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id=? OR name=?`, id, id)
	if err != nil {
		return fmt.Errorf("remove project: %w", err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) SaveRun(ctx context.Context, run Run) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO runs(project_id,pid,port,log_path,started_at) VALUES(?,?,?,?,?) ON CONFLICT(project_id) DO UPDATE SET pid=excluded.pid,port=excluded.port,log_path=excluded.log_path,started_at=excluded.started_at`, run.ProjectID, run.PID, run.Port, run.LogPath, run.StartedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) Run(ctx context.Context, projectID string) (*Run, error) {
	var run Run
	var started string
	err := s.db.QueryRowContext(ctx, `SELECT project_id,pid,port,log_path,started_at FROM runs WHERE project_id=?`, projectID).Scan(&run.ProjectID, &run.PID, &run.Port, &run.LogPath, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	run.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
	return &run, nil
}

func (s *SQLiteStore) ClearRun(ctx context.Context, projectID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM runs WHERE project_id=?`, projectID)
	return err
}

func (s *SQLiteStore) SetLastPort(ctx context.Context, projectID string, port int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE projects SET last_port=?,updated_at=? WHERE id=?`, port, time.Now().UTC().Format(time.RFC3339Nano), projectID)
	return err
}

func (s *SQLiteStore) MoveProject(ctx context.Context, projectID string, direction int) (bool, error) {
	if direction != -1 && direction != 1 {
		return false, errors.New("project move direction must be -1 or 1")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var currentPosition int
	if err := tx.QueryRowContext(ctx, `SELECT position FROM projects WHERE id=?`, projectID).Scan(&currentPosition); errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	} else if err != nil {
		return false, err
	}
	operator, order := "<", "DESC"
	if direction > 0 {
		operator, order = ">", "ASC"
	}
	query := fmt.Sprintf(`SELECT id,position FROM projects WHERE position %s ? ORDER BY position %s LIMIT 1`, operator, order)
	var neighborID string
	var neighborPosition int
	if err := tx.QueryRowContext(ctx, query, currentPosition).Scan(&neighborID, &neighborPosition); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET position=? WHERE id=?`, neighborPosition, projectID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET position=? WHERE id=?`, currentPosition, neighborID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }
