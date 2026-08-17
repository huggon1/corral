package app

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHelperServer(t *testing.T) {
	if os.Getenv("LHM_HELPER_SERVER") != "1" {
		return
	}
	args := os.Args
	port := args[len(args)-1]
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		os.Exit(2)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	if readyPort := os.Getenv("LHM_HELPER_READY_PORT"); readyPort != "" {
		readyListener, err := net.Listen("tcp", "127.0.0.1:"+readyPort)
		if err != nil {
			os.Exit(3)
		}
		go func() { _ = http.Serve(readyListener, handler) }()
	}
	_ = http.Serve(listener, handler)
	os.Exit(0)
}

func TestOpenManagerMigratesLegacyCorralData(t *testing.T) {
	config := t.TempDir()
	previousUserConfigDir := userConfigDir
	userConfigDir = func() (string, error) { return config, nil }
	t.Cleanup(func() { userConfigDir = previousUserConfigDir })
	t.Setenv("LHM_HOME", "")

	legacyHome := filepath.Join(config, "corral")
	legacyPaths := Paths{Home: legacyHome, Database: filepath.Join(legacyHome, "corral.db"), Logs: filepath.Join(legacyHome, "logs")}
	legacyStore, err := OpenSQLite(legacyPaths.Database)
	if err != nil {
		t.Fatal(err)
	}
	legacyManager := NewManager(legacyStore, legacyPaths)
	project, err := legacyManager.Upsert(context.Background(), ProjectSpec{
		Name:        "legacy-project",
		Path:        t.TempDir(),
		Command:     []string{"npm", "run", "dev"},
		URLTemplate: "http://127.0.0.1:4317",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(legacyPaths.Logs, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyLog := filepath.Join(legacyPaths.Logs, project.ID+".log")
	if err := os.WriteFile(legacyLog, []byte("legacy log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := legacyManager.Close(); err != nil {
		t.Fatal(err)
	}

	manager, err := OpenManager()
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	states, err := manager.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Project.ID != project.ID {
		t.Fatalf("migrated projects = %#v", states)
	}
	contents, err := os.ReadFile(filepath.Join(manager.paths.Logs, project.ID+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "legacy log\n" {
		t.Fatalf("migrated log = %q", contents)
	}
}

func TestManagerLifecycle(t *testing.T) {
	home := t.TempDir()
	paths := Paths{Home: home, Database: filepath.Join(home, "lhm.db"), Logs: filepath.Join(home, "logs")}
	store, err := OpenSQLite(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store, paths)
	defer manager.Close()
	ctx := context.Background()
	project, err := manager.Upsert(ctx, ProjectSpec{
		Name:        "helper",
		Path:        t.TempDir(),
		Command:     []string{os.Args[0], "-test.run=TestHelperServer", "--", "{port}"},
		URLTemplate: "http://localhost:{port}",
		Env:         map[string]string{"LHM_HELPER_SERVER": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := manager.Start(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != StatusRunning || state.Run == nil {
		t.Fatalf("state = %#v", state)
	}
	if state.Run.Port <= 0 {
		t.Fatalf("invalid port %d", state.Run.Port)
	}
	if err := manager.Stop(ctx, project.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for processRunning(state.Run.PID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	stopped, err := manager.State(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != StatusStopped {
		t.Fatalf("status = %s", stopped.Status)
	}
}

func TestManagerLifecycleWithFixedURLAndSeparateReadyURL(t *testing.T) {
	home := t.TempDir()
	paths := Paths{Home: home, Database: filepath.Join(home, "lhm.db"), Logs: filepath.Join(home, "logs")}
	store, err := OpenSQLite(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store, paths)
	defer manager.Close()
	ctx := context.Background()
	publicPort := availablePort(t)
	readyPort := availablePort(t)
	for readyPort == publicPort {
		readyPort = availablePort(t)
	}
	project, err := manager.Upsert(ctx, ProjectSpec{
		Name:             "fixed-helper",
		Path:             t.TempDir(),
		Command:          []string{os.Args[0], "-test.run=TestHelperServer", "--", strconv.Itoa(publicPort)},
		URLTemplate:      "http://127.0.0.1:" + strconv.Itoa(publicPort) + "/app",
		ReadyURLTemplate: "http://127.0.0.1:" + strconv.Itoa(readyPort) + "/health",
		Env: map[string]string{
			"LHM_HELPER_SERVER":     "1",
			"LHM_HELPER_READY_PORT": strconv.Itoa(readyPort),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := manager.Start(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != StatusRunning || state.Run == nil {
		t.Fatalf("state = %#v", state)
	}
	if state.Run.Port != 0 {
		t.Fatalf("fixed project allocated port %d", state.Run.Port)
	}
	if state.URL != project.URLTemplate {
		t.Fatalf("URL = %q", state.URL)
	}
	if err := manager.Stop(ctx, project.ID); err != nil {
		t.Fatal(err)
	}
}

func availablePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestManagerRejectsFixedReadyURLAlreadyInUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("already running"))
	}))
	defer server.Close()

	home := t.TempDir()
	paths := Paths{Home: home, Database: filepath.Join(home, "lhm.db"), Logs: filepath.Join(home, "logs")}
	store, err := OpenSQLite(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store, paths)
	defer manager.Close()
	project, err := manager.Upsert(context.Background(), ProjectSpec{
		Path:        t.TempDir(),
		Command:     []string{"unused-command"},
		URLTemplate: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Start(context.Background(), project.ID)
	if err == nil || !strings.Contains(err.Error(), "already responding") {
		t.Fatalf("error = %v", err)
	}
}

func TestManagerClearsLastPortWhenProjectBecomesFixed(t *testing.T) {
	home := t.TempDir()
	paths := Paths{Home: home, Database: filepath.Join(home, "lhm.db"), Logs: filepath.Join(home, "logs")}
	store, err := OpenSQLite(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store, paths)
	defer manager.Close()
	ctx := context.Background()
	projectPath := t.TempDir()
	dynamic, err := manager.Upsert(ctx, ProjectSpec{Path: projectPath, Command: []string{"serve", "{port}"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetLastPort(ctx, dynamic.ID, 4567); err != nil {
		t.Fatal(err)
	}
	fixed, err := manager.Upsert(ctx, ProjectSpec{
		Path:        projectPath,
		Command:     []string{"npm", "run", "dev"},
		URLTemplate: "http://127.0.0.1:4317",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fixed.LastPort != 0 {
		t.Fatalf("last port = %d", fixed.LastPort)
	}
}

func TestManagerMovesProjectsAndPersistsOrder(t *testing.T) {
	home := t.TempDir()
	paths := Paths{Home: home, Database: filepath.Join(home, "lhm.db"), Logs: filepath.Join(home, "logs")}
	store, err := OpenSQLite(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store, paths)
	ctx := context.Background()
	var ids []string
	for _, name := range []string{"zeta", "alpha", "middle"} {
		project, err := manager.Upsert(ctx, ProjectSpec{
			Name:        name,
			Path:        t.TempDir(),
			Command:     []string{"npm", "run", "dev"},
			URLTemplate: "http://127.0.0.1:3000",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, project.ID)
	}
	assertProjectOrder(t, manager, []string{"zeta", "alpha", "middle"})

	moved, err := manager.Move(ctx, ids[2], -1)
	if err != nil || !moved {
		t.Fatalf("move up: moved=%v err=%v", moved, err)
	}
	assertProjectOrder(t, manager, []string{"zeta", "middle", "alpha"})

	moved, err = manager.Move(ctx, ids[0], -1)
	if err != nil || moved {
		t.Fatalf("move at top: moved=%v err=%v", moved, err)
	}
	moved, err = manager.Move(ctx, ids[2], 1)
	if err != nil || !moved {
		t.Fatalf("move down: moved=%v err=%v", moved, err)
	}
	assertProjectOrder(t, manager, []string{"zeta", "alpha", "middle"})

	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedStore, err := OpenSQLite(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	reopened := NewManager(reopenedStore, paths)
	defer reopened.Close()
	assertProjectOrder(t, reopened, []string{"zeta", "alpha", "middle"})
}

func assertProjectOrder(t *testing.T, manager *Manager, want []string) {
	t.Helper()
	states, err := manager.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(states))
	for index := range states {
		got[index] = states[index].Project.Name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
