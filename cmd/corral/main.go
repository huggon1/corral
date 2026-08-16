package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/huggon1/corral/internal/app"
	"github.com/huggon1/corral/internal/tui"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "corral:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	manager, err := app.OpenManager()
	if err != nil {
		return err
	}
	defer manager.Close()
	if len(args) == 0 || args[0] == "tui" {
		return tui.Run(manager)
	}
	ctx := context.Background()
	switch args[0] {
	case "register", "upsert":
		return register(ctx, manager, args[1:])
	case "list", "ls":
		return list(ctx, manager, args[1:])
	case "status":
		return status(ctx, manager, args[1:])
	case "start":
		return start(ctx, manager, args[1:])
	case "stop":
		return stop(ctx, manager, args[1:])
	case "restart":
		return restart(ctx, manager, args[1:])
	case "logs":
		return logs(ctx, manager, args[1:])
	case "remove", "rm":
		return remove(ctx, manager, args[1:])
	case "version", "--version", "-v":
		fmt.Println("corral", version)
		return nil
	case "help", "--help", "-h":
		printHelp()
		return nil
	default:
		return fmt.Errorf("unknown command %q; run corral help", args[0])
	}
}

type envFlags map[string]string

func (e envFlags) String() string { return "KEY=VALUE" }
func (e envFlags) Set(value string) error {
	key, val, ok := strings.Cut(value, "=")
	if !ok || key == "" {
		return errors.New("environment must be KEY=VALUE")
	}
	e[key] = val
	return nil
}

func register(ctx context.Context, manager *app.Manager, args []string) error {
	flags := flag.NewFlagSet("register", flag.ContinueOnError)
	name := flags.String("name", "", "project display name")
	path := flags.String("path", ".", "project directory")
	url := flags.String("url", "", "URL opened for the project (defaults to http://localhost:{port} for dynamic-port commands)")
	readyURL := flags.String("ready-url", "", "HTTP readiness URL (defaults to --url)")
	env := envFlags{}
	flags.Var(env, "env", "non-secret environment KEY=VALUE (repeatable)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	command := flags.Args()
	if len(command) > 0 && command[0] == "--" {
		command = command[1:]
	}
	project, err := manager.Upsert(ctx, app.ProjectSpec{Name: *name, Path: *path, Command: command, URLTemplate: *url, ReadyURLTemplate: *readyURL, Env: env})
	if err != nil {
		return err
	}
	fmt.Printf("Registered %s (%s)\n", project.Name, project.ID)
	return nil
}

func list(ctx context.Context, manager *app.Manager, args []string) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	states, err := manager.Projects(ctx)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(states)
	}
	if len(states) == 0 {
		fmt.Println("No projects registered.")
		return nil
	}
	for _, state := range states {
		port := "-"
		if state.Run != nil {
			port = fmt.Sprint(state.Run.Port)
		}
		fmt.Printf("%-22s %-10s %-6s %s\n", state.Project.Name, state.Status, port, state.Project.Path)
	}
	return nil
}

func status(ctx context.Context, manager *app.Manager, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: corral status <project>")
	}
	state, err := manager.State(ctx, args[0])
	if err != nil {
		return err
	}
	return printJSON(state)
}

func start(ctx context.Context, manager *app.Manager, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: corral start <project>")
	}
	state, err := manager.Start(ctx, args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Started %s at %s\n", state.Project.Name, state.URL)
	return nil
}

func stop(ctx context.Context, manager *app.Manager, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: corral stop <project>")
	}
	if err := manager.Stop(ctx, args[0]); err != nil {
		return err
	}
	fmt.Println("Stopped", args[0])
	return nil
}

func restart(ctx context.Context, manager *app.Manager, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: corral restart <project>")
	}
	state, err := manager.Restart(ctx, args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Restarted %s at %s\n", state.Project.Name, state.URL)
	return nil
}

func logs(ctx context.Context, manager *app.Manager, args []string) error {
	flags := flag.NewFlagSet("logs", flag.ContinueOnError)
	lines := flags.Int("n", 100, "number of lines")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: corral logs [-n lines] <project>")
	}
	output, err := manager.Logs(ctx, flags.Arg(0), *lines)
	if err != nil {
		return err
	}
	fmt.Println(output)
	return nil
}

func remove(ctx context.Context, manager *app.Manager, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: corral remove <project>")
	}
	if err := manager.Remove(ctx, args[0]); err != nil {
		return err
	}
	fmt.Println("Removed", args[0])
	return nil
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printHelp() {
	fmt.Print(`corral — round up and launch local development projects

Usage:
  corral                                      Open the TUI
  corral register --path DIR --url URL [options] -- COMMAND ...
  corral list [--json]
  corral start|stop|restart|status <project>
  corral logs [-n lines] <project>
  corral remove <project>

Register example:
  corral register --path . --name web --url http://localhost:3000 -- npm run dev
  corral register --path . --name web -- npm run dev -- --port {port}
`)
}
