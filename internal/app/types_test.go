package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeSpec(t *testing.T) {
	dir := t.TempDir()
	spec, err := NormalizeSpec(ProjectSpec{Path: dir, Command: []string{"npm", "run", "dev", "--", "--port", "{port}"}})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != filepath.Base(dir) {
		t.Fatalf("name = %q", spec.Name)
	}
	if spec.URLTemplate != "http://localhost:{port}" {
		t.Fatalf("url = %q", spec.URLTemplate)
	}
	if spec.ReadyURLTemplate != spec.URLTemplate {
		t.Fatalf("ready URL = %q", spec.ReadyURLTemplate)
	}
	expectedPath, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != expectedPath {
		t.Fatalf("path = %q", spec.Path)
	}
}

func TestNormalizeSpecAcceptsFixedURLWithoutPortPlaceholder(t *testing.T) {
	spec, err := NormalizeSpec(ProjectSpec{
		Path:             t.TempDir(),
		Command:          []string{"npm", "run", "dev"},
		URLTemplate:      "http://127.0.0.1:4317",
		ReadyURLTemplate: "http://127.0.0.1:4318/health",
	})
	if err != nil {
		t.Fatal(err)
	}
	if UsesDynamicPort(spec) {
		t.Fatal("fixed project unexpectedly uses a dynamic port")
	}
	if spec.ReadyURLTemplate != "http://127.0.0.1:4318/health" {
		t.Fatalf("ready URL = %q", spec.ReadyURLTemplate)
	}
}

func TestNormalizeSpecRequiresURLForFixedCommand(t *testing.T) {
	_, err := NormalizeSpec(ProjectSpec{Path: t.TempDir(), Command: []string{"npm", "run", "dev"}})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestNormalizeSpecAcceptsPortInEnvironment(t *testing.T) {
	spec, err := NormalizeSpec(ProjectSpec{
		Path:        t.TempDir(),
		Command:     []string{"npm", "run", "dev"},
		URLTemplate: "http://localhost:{port}",
		Env:         map[string]string{"PORT": "{port}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !UsesDynamicPort(spec) {
		t.Fatal("environment placeholder was not detected")
	}
}

func TestNormalizeSpecRejectsUnboundURLPort(t *testing.T) {
	_, err := NormalizeSpec(ProjectSpec{
		Path:        t.TempDir(),
		Command:     []string{"npm", "run", "dev"},
		URLTemplate: "http://localhost:{port}",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestExpandPortDoesNotMutateInput(t *testing.T) {
	input := []string{"serve", "--port={port}"}
	got := ExpandPort(input, 4567)
	want := []string{"serve", "--port=4567"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if input[1] != "--port={port}" {
		t.Fatal("input mutated")
	}
}

func TestExpandPortEnvironmentDoesNotMutateInput(t *testing.T) {
	input := map[string]string{"PORT": "{port}", "MODE": "dev"}
	got := ExpandPortEnvironment(input, 4567)
	want := map[string]string{"PORT": "4567", "MODE": "dev"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if input["PORT"] != "{port}" {
		t.Fatal("input mutated")
	}
}

func TestProjectIDStableAcrossSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(dir, link); err != nil {
		t.Skip(err)
	}
	a, err := NormalizeSpec(ProjectSpec{Path: dir, Command: []string{"serve", "{port}"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NormalizeSpec(ProjectSpec{Path: link, Command: []string{"serve", "{port}"}})
	if err != nil {
		t.Fatal(err)
	}
	if ProjectID(a.Path) != ProjectID(b.Path) {
		t.Fatal("symlink produced a different project ID")
	}
}
