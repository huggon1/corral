package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/huggon1/localhost-manager/internal/app"
)

func TestRenderFitsTerminal(t *testing.T) {
	home := t.TempDir()
	paths := app.Paths{Home: home, Database: filepath.Join(home, "lhm.db"), Logs: filepath.Join(home, "logs")}
	store, err := app.OpenSQLite(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	manager := app.NewManager(store, paths)
	defer manager.Close()
	_, err = manager.Upsert(context.Background(), app.ProjectSpec{
		Name: "example", Path: t.TempDir(), Command: []string{"serve", "--port", "{port}"}, URLTemplate: "http://localhost:{port}",
	})
	if err != nil {
		t.Fatal(err)
	}
	states, err := manager.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []struct{ width, height int }{{80, 24}, {120, 36}} {
		model := New(manager)
		model.width, model.height, model.states = size.width, size.height, states
		rendered := model.render()
		if got := lipgloss.Width(rendered); got > size.width {
			t.Errorf("render width = %d, terminal width = %d", got, size.width)
		}
		if got := lipgloss.Height(rendered); got > size.height {
			t.Errorf("render height = %d, terminal height = %d", got, size.height)
		}
		if !strings.Contains(rendered, "Reorder") && !strings.Contains(rendered, "Order") {
			t.Errorf("render at %dx%d clipped the footer", size.width, size.height)
		}
	}
}

func TestHeaderDoesNotWrap(t *testing.T) {
	for _, width := range []int{50, 80, 120} {
		model := &Model{}
		header := model.renderHeader(width)
		if got := lipgloss.Height(header); got != 3 {
			t.Errorf("header height at width %d = %d; rendered %q", width, got, header)
		}
		if got := lipgloss.Width(header); got > width {
			t.Errorf("header width at width %d = %d", width, got)
		}
	}
}

func TestFooterDoesNotWrap(t *testing.T) {
	for _, width := range []int{50, 80, 120} {
		model := &Model{}
		footer := model.renderFooter(width)
		if got := lipgloss.Height(footer); got != 2 {
			t.Errorf("footer height at width %d = %d; rendered %q", width, got, footer)
		}
		if got := lipgloss.Width(footer); got > width {
			t.Errorf("footer width at width %d = %d", width, got)
		}
	}
}

func TestStateMetaOmitsUnallocatedPort(t *testing.T) {
	state := app.ProjectState{Status: app.StatusRunning, Run: &app.Run{Port: 0}}
	if got := stateMeta(state); got != "Running" {
		t.Fatalf("state meta = %q", got)
	}
}

func TestRenderUsesTextBrandAndDeleteConfirmation(t *testing.T) {
	model := &Model{
		width:             100,
		height:            30,
		confirmRemoveID:   "project-id",
		confirmRemoveName: "example",
	}
	rendered := model.render()
	if strings.Contains(rendered, "🐎") {
		t.Fatal("render still contains the old horse icon")
	}
	if !strings.Contains(rendered, "Remove example?") || !strings.Contains(rendered, "Confirm") || !strings.Contains(rendered, "Cancel") {
		t.Fatalf("confirmation is missing from render: %q", rendered)
	}
	if got := lipgloss.Width(rendered); got > model.width {
		t.Fatalf("confirmation render width = %d, terminal width = %d", got, model.width)
	}
}

func TestDeleteRequiresConfirmation(t *testing.T) {
	home := t.TempDir()
	paths := app.Paths{Home: home, Database: filepath.Join(home, "lhm.db"), Logs: filepath.Join(home, "logs")}
	store, err := app.OpenSQLite(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	manager := app.NewManager(store, paths)
	defer manager.Close()
	project, err := manager.Upsert(context.Background(), app.ProjectSpec{
		Name: "example", Path: t.TempDir(), Command: []string{"serve"}, URLTemplate: "http://127.0.0.1:3000",
	})
	if err != nil {
		t.Fatal(err)
	}
	states, err := manager.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	model := New(manager)
	model.states = states

	_, command := model.handleKey(keyPress("d"))
	if command != nil || model.confirmRemoveID != project.ID {
		t.Fatalf("delete did not enter confirmation: id=%q command=%v", model.confirmRemoveID, command)
	}
	if _, err := manager.State(context.Background(), project.ID); err != nil {
		t.Fatalf("project was removed before confirmation: %v", err)
	}

	_, command = model.handleKey(keyPress("y"))
	if command == nil {
		t.Fatal("confirmation did not schedule removal")
	}
	message := command()
	if result, ok := message.(actionMsg); !ok || result.err != nil {
		t.Fatalf("remove result = %#v", message)
	}
	if _, err := manager.State(context.Background(), project.ID); err == nil {
		t.Fatal("project still exists after confirmation")
	}
}

func TestReorderModeMovesSelectedProjectWithVimKey(t *testing.T) {
	home := t.TempDir()
	paths := app.Paths{Home: home, Database: filepath.Join(home, "lhm.db"), Logs: filepath.Join(home, "logs")}
	store, err := app.OpenSQLite(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	manager := app.NewManager(store, paths)
	defer manager.Close()
	for _, name := range []string{"first", "second"} {
		if _, err := manager.Upsert(context.Background(), app.ProjectSpec{
			Name: name, Path: t.TempDir(), Command: []string{"serve"}, URLTemplate: "http://127.0.0.1:3000",
		}); err != nil {
			t.Fatal(err)
		}
	}
	states, err := manager.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	model := New(manager)
	model.states = states
	model.cursor = 1
	_, command := model.handleKey(keyPress(" "))
	if command != nil || !model.reorderMode {
		t.Fatalf("space did not enter reorder mode: mode=%v command=%v", model.reorderMode, command)
	}
	_, command = model.handleKey(keyPress("k"))
	if command == nil {
		t.Fatal("k did not schedule a move in reorder mode")
	}
	message := command()
	if result, ok := message.(actionMsg); !ok || result.err != nil {
		t.Fatalf("move result = %#v", message)
	}
	reordered, err := manager.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reordered[0].Project.Name != "second" || reordered[1].Project.Name != "first" {
		t.Fatalf("order = %s, %s", reordered[0].Project.Name, reordered[1].Project.Name)
	}
	_, command = model.handleKey(keyCode(tea.KeyEnter))
	if command != nil || model.reorderMode {
		t.Fatalf("enter did not exit reorder mode: mode=%v command=%v", model.reorderMode, command)
	}
}

func keyPress(value string) tea.KeyPressMsg {
	runes := []rune(value)
	return tea.KeyPressMsg(tea.Key{Text: value, Code: runes[0]})
}

func keyCode(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}
