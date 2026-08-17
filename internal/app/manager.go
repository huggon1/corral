package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var userConfigDir = os.UserConfigDir

type Paths struct {
	Home     string
	Database string
	Logs     string
}

func DefaultPaths() (Paths, error) {
	home := os.Getenv("LHM_HOME")
	if home == "" {
		config, err := userConfigDir()
		if err != nil {
			return Paths{}, fmt.Errorf("find user config directory: %w", err)
		}
		home = filepath.Join(config, "localhost-manager")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return Paths{}, err
	}
	return Paths{Home: abs, Database: filepath.Join(abs, "lhm.db"), Logs: filepath.Join(abs, "logs")}, nil
}

func legacyPaths() (Paths, bool, error) {
	if os.Getenv("LHM_HOME") != "" {
		return Paths{}, false, nil
	}
	config, err := userConfigDir()
	if err != nil {
		return Paths{}, false, fmt.Errorf("find user config directory: %w", err)
	}
	home := filepath.Join(config, "corral")
	return Paths{Home: home, Database: filepath.Join(home, "corral.db"), Logs: filepath.Join(home, "logs")}, true, nil
}

type Manager struct {
	store Store
	paths Paths
	now   func() time.Time
}

func NewManager(store Store, paths Paths) *Manager {
	return &Manager{store: store, paths: paths, now: time.Now}
}

func OpenManager() (*Manager, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}
	databaseExists, err := fileExists(paths.Database)
	if err != nil {
		return nil, err
	}
	store, err := OpenSQLite(paths.Database)
	if err != nil {
		return nil, err
	}
	if !databaseExists {
		legacy, shouldMigrate, err := legacyPaths()
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		if shouldMigrate {
			legacyExists, err := fileExists(legacy.Database)
			if err != nil {
				_ = store.Close()
				return nil, err
			}
			if legacyExists {
				if err := store.ImportLegacy(legacy.Database); err != nil {
					_ = store.Close()
					return nil, fmt.Errorf("migrate legacy Corral registry: %w", err)
				}
				if err := copyDirectory(legacy.Logs, paths.Logs); err != nil {
					_ = store.Close()
					return nil, fmt.Errorf("migrate legacy Corral logs: %w", err)
				}
			}
		}
	}
	return NewManager(store, paths), nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func copyDirectory(source, destination string) error {
	entries, err := os.ReadDir(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(source, entry.Name())
		destinationPath := filepath.Join(destination, entry.Name())
		if entry.IsDir() {
			if err := copyDirectory(sourcePath, destinationPath); err != nil {
				return err
			}
			continue
		}
		if !entry.Type().IsRegular() {
			continue
		}
		exists, err := fileExists(destinationPath)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := copyFile(sourcePath, destinationPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (m *Manager) Close() error { return m.store.Close() }

func (m *Manager) Upsert(ctx context.Context, spec ProjectSpec) (Project, error) {
	normalized, err := NormalizeSpec(spec)
	if err != nil {
		return Project{}, err
	}
	project, err := m.store.UpsertProject(ctx, normalized)
	if err != nil {
		return Project{}, err
	}
	if !UsesDynamicPort(normalized) && project.LastPort != 0 {
		if err := m.store.SetLastPort(ctx, project.ID, 0); err != nil {
			return Project{}, fmt.Errorf("clear dynamic port: %w", err)
		}
		project.LastPort = 0
	}
	return project, nil
}

func (m *Manager) Projects(ctx context.Context) ([]ProjectState, error) {
	projects, err := m.store.Projects(ctx)
	if err != nil {
		return nil, err
	}
	states := make([]ProjectState, 0, len(projects))
	for _, project := range projects {
		state, err := m.state(ctx, project)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, nil
}

func (m *Manager) Move(ctx context.Context, id string, direction int) (bool, error) {
	project, err := m.store.Project(ctx, id)
	if err != nil {
		return false, err
	}
	return m.store.MoveProject(ctx, project.ID, direction)
}

func (m *Manager) State(ctx context.Context, id string) (ProjectState, error) {
	project, err := m.store.Project(ctx, id)
	if err != nil {
		return ProjectState{}, err
	}
	return m.state(ctx, project)
}

func (m *Manager) state(ctx context.Context, project Project) (ProjectState, error) {
	run, err := m.store.Run(ctx, project.ID)
	if err != nil {
		return ProjectState{}, err
	}
	state := ProjectState{Project: project, Status: StatusStopped, Run: run}
	if run == nil {
		return state, nil
	}
	state.URL = ExpandURL(project.URLTemplate, run.Port)
	readyURL := ExpandURL(project.ReadyURLTemplate, run.Port)
	if !processRunning(run.PID) {
		state.Status = StatusCrashed
		return state, nil
	}
	if endpointReady(ctx, readyURL, 150*time.Millisecond) {
		state.Status = StatusRunning
	} else if m.now().Sub(run.StartedAt) < 30*time.Second {
		state.Status = StatusStarting
	} else {
		state.Status = StatusUnhealthy
	}
	return state, nil
}

func (m *Manager) Start(ctx context.Context, id string) (ProjectState, error) {
	project, err := m.store.Project(ctx, id)
	if err != nil {
		return ProjectState{}, err
	}
	current, err := m.state(ctx, project)
	if err != nil {
		return ProjectState{}, err
	}
	if current.Status == StatusRunning || current.Status == StatusStarting {
		return current, nil
	}
	if current.Run != nil && processRunning(current.Run.PID) {
		_ = stopProcess(current.Run.PID, 3*time.Second)
	}
	_ = m.store.ClearRun(ctx, project.ID)

	usesDynamicPort := UsesDynamicPort(ProjectSpec{Command: project.Command, Env: project.Env})
	port := 0
	if usesDynamicPort {
		port, err = allocatePort(project.LastPort)
		if err != nil {
			return ProjectState{}, err
		}
	}
	readyURL := ExpandURL(project.ReadyURLTemplate, port)
	if !strings.Contains(project.ReadyURLTemplate, "{port}") && isLocalHTTPURL(readyURL) && endpointReady(ctx, readyURL, 150*time.Millisecond) {
		return ProjectState{}, fmt.Errorf("ready URL %s was already responding before launch", readyURL)
	}
	if err := os.MkdirAll(m.paths.Logs, 0o755); err != nil {
		return ProjectState{}, fmt.Errorf("create log directory: %w", err)
	}
	logPath := filepath.Join(m.paths.Logs, project.ID+".log")
	args := ExpandPort(project.Command, port)
	env := ExpandPortEnvironment(project.Env, port)
	pid, err := startProcess(project.Path, args, env, logPath)
	if err != nil {
		return ProjectState{}, err
	}
	run := Run{ProjectID: project.ID, PID: pid, Port: port, LogPath: logPath, StartedAt: m.now().UTC()}
	if err := m.store.SaveRun(ctx, run); err != nil {
		_ = stopProcess(pid, time.Second)
		return ProjectState{}, fmt.Errorf("save run: %w", err)
	}
	if port > 0 {
		_ = m.store.SetLastPort(ctx, project.ID, port)
	}

	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ProjectState{}, ctx.Err()
		case <-deadline.C:
			_ = stopProcess(pid, 2*time.Second)
			return ProjectState{}, fmt.Errorf("%s did not become ready at %s within 20s", project.Name, readyURL)
		case <-ticker.C:
			if !processRunning(pid) {
				return ProjectState{}, fmt.Errorf("%s exited before becoming ready; see %s", project.Name, logPath)
			}
			if endpointReady(ctx, readyURL, 500*time.Millisecond) {
				if port > 0 {
					project.LastPort = port
				}
				return ProjectState{Project: project, Status: StatusRunning, Run: &run, URL: ExpandURL(project.URLTemplate, port)}, nil
			}
		}
	}
}

func (m *Manager) Stop(ctx context.Context, id string) error {
	project, err := m.store.Project(ctx, id)
	if err != nil {
		return err
	}
	run, err := m.store.Run(ctx, project.ID)
	if err != nil || run == nil {
		return err
	}
	if processRunning(run.PID) {
		if err := stopProcess(run.PID, 4*time.Second); err != nil {
			return fmt.Errorf("stop %s: %w", project.Name, err)
		}
	}
	return m.store.ClearRun(ctx, project.ID)
}

func (m *Manager) Restart(ctx context.Context, id string) (ProjectState, error) {
	if err := m.Stop(ctx, id); err != nil && !errors.Is(err, ErrNotFound) {
		return ProjectState{}, err
	}
	return m.Start(ctx, id)
}

func (m *Manager) Remove(ctx context.Context, id string) error {
	project, err := m.store.Project(ctx, id)
	if err != nil {
		return err
	}
	if err := m.Stop(ctx, project.ID); err != nil {
		return err
	}
	return m.store.RemoveProject(ctx, project.ID)
}

func (m *Manager) Logs(ctx context.Context, id string, lines int) (string, error) {
	project, err := m.store.Project(ctx, id)
	if err != nil {
		return "", err
	}
	path := filepath.Join(m.paths.Logs, project.ID+".log")
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "No logs yet.", nil
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	if lines <= 0 {
		lines = 100
	}
	scanner := bufio.NewScanner(file)
	buffer := make([]string, 0, lines)
	for scanner.Scan() {
		if len(buffer) == lines {
			copy(buffer, buffer[1:])
			buffer[len(buffer)-1] = scanner.Text()
		} else {
			buffer = append(buffer, scanner.Text())
		}
	}
	return strings.Join(buffer, "\n"), scanner.Err()
}

func allocatePort(preferred int) (int, error) {
	if preferred > 0 && portAvailable(preferred) {
		return preferred, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, err
	}
	return port, nil
}

func portAvailable(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func endpointReady(ctx context.Context, target string, timeout time.Duration) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode < http.StatusInternalServerError
}

func isLocalHTTPURL(target string) bool {
	parsed, err := url.Parse(target)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
