package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Status string

const (
	StatusStopped   Status = "stopped"
	StatusStarting  Status = "starting"
	StatusRunning   Status = "running"
	StatusUnhealthy Status = "unhealthy"
	StatusCrashed   Status = "crashed"
)

type ProjectSpec struct {
	Name             string            `json:"name"`
	Path             string            `json:"path"`
	Command          []string          `json:"command"`
	URLTemplate      string            `json:"url_template"`
	ReadyURLTemplate string            `json:"ready_url_template"`
	Env              map[string]string `json:"env,omitempty"`
}

type Project struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Path             string            `json:"path"`
	Command          []string          `json:"command"`
	URLTemplate      string            `json:"url_template"`
	ReadyURLTemplate string            `json:"ready_url_template"`
	Env              map[string]string `json:"env,omitempty"`
	LastPort         int               `json:"last_port,omitempty"`
	Position         int               `json:"position"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type Run struct {
	ProjectID string    `json:"project_id"`
	PID       int       `json:"pid"`
	Port      int       `json:"port,omitempty"`
	LogPath   string    `json:"log_path"`
	StartedAt time.Time `json:"started_at"`
}

type ProjectState struct {
	Project Project `json:"project"`
	Status  Status  `json:"status"`
	Run     *Run    `json:"run,omitempty"`
	URL     string  `json:"url,omitempty"`
}

func NormalizeSpec(spec ProjectSpec) (ProjectSpec, error) {
	if strings.TrimSpace(spec.Path) == "" {
		return spec, errors.New("project path is required")
	}
	abs, err := filepath.Abs(spec.Path)
	if err != nil {
		return spec, fmt.Errorf("resolve project path: %w", err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return spec, fmt.Errorf("project path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return spec, fmt.Errorf("project path: %w", err)
	}
	if !info.IsDir() {
		return spec, errors.New("project path must be a directory")
	}
	if len(spec.Command) == 0 || strings.TrimSpace(spec.Command[0]) == "" {
		return spec, errors.New("project command is required after --")
	}
	usesDynamicPort := UsesDynamicPort(spec)
	if spec.URLTemplate == "" {
		if !usesDynamicPort {
			return spec, errors.New("project URL is required when the command does not use {port}")
		}
		spec.URLTemplate = "http://localhost:{port}"
	}
	if !usesDynamicPort && (strings.Contains(spec.URLTemplate, "{port}") || strings.Contains(spec.ReadyURLTemplate, "{port}")) {
		return spec, errors.New("{port} in a URL must also be passed through the command or environment")
	}
	if spec.ReadyURLTemplate == "" {
		spec.ReadyURLTemplate = spec.URLTemplate
	}
	if err := validateHTTPURLTemplate("project URL", spec.URLTemplate); err != nil {
		return spec, err
	}
	if err := validateHTTPURLTemplate("ready URL", spec.ReadyURLTemplate); err != nil {
		return spec, err
	}
	spec.Path = filepath.Clean(abs)
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Name == "" {
		spec.Name = filepath.Base(spec.Path)
	}
	if spec.Env == nil {
		spec.Env = map[string]string{}
	}
	return spec, nil
}

func UsesDynamicPort(spec ProjectSpec) bool {
	for _, value := range spec.Command {
		if strings.Contains(value, "{port}") {
			return true
		}
	}
	for _, value := range spec.Env {
		if strings.Contains(value, "{port}") {
			return true
		}
	}
	return false
}

func validateHTTPURLTemplate(label, value string) error {
	resolved := strings.ReplaceAll(value, "{port}", "43210")
	parsed, err := url.ParseRequestURI(resolved)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute http or https URL", label)
	}
	return nil
}

func ProjectID(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:8])
}

func ExpandPort(values []string, port int) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = strings.ReplaceAll(value, "{port}", fmt.Sprint(port))
	}
	return out
}

func ExpandURL(template string, port int) string {
	return strings.ReplaceAll(template, "{port}", fmt.Sprint(port))
}

func ExpandPortEnvironment(values map[string]string, port int) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = strings.ReplaceAll(value, "{port}", fmt.Sprint(port))
	}
	return out
}
