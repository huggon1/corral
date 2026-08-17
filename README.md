<div align="center">

# localhost-manager

### Round up every local project. Launch anything in one keystroke.

[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev/)
[![Bubble Tea](https://img.shields.io/badge/TUI-Bubble_Tea-FF69B4?style=for-the-badge)](https://github.com/charmbracelet/bubbletea)
![Platforms](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-7C3AED?style=for-the-badge)
![Status](https://img.shields.io/badge/status-early_alpha-F59E0B?style=for-the-badge)

**English** · [简体中文](README.zh-CN.md)

</div>

---

localhost-manager is a global TUI launcher for local development projects. Your coding agent keeps the project catalog current; you open localhost-manager from anywhere and press **Enter** to start the app you need.

No more remembering directories. No more hunting through `package.json`. No more localhost port collisions. ✨

## 🌟 Why localhost-manager?

When several projects are in motion, their startup knowledge becomes scattered across shell history, README files, package scripts, and your own memory. localhost-manager turns that knowledge into a small, reliable catalog.

- 🧠 **Forget the command** — store the verified startup command once.
- 🗺️ **Launch from anywhere** — every project runs in its registered working directory.
- 🚦 **Avoid port collisions** — reuse the last port when possible or allocate a free one.
- 🪄 **Agent-maintained** — the portable Agent Skill registers projects after verifying them.
- 🧵 **Control the whole process tree** — start, stop, and restart detached development servers.
- 📜 **Keep logs close** — inspect recent output without leaving the TUI.
- ↕️ **Arrange your workspace** — move projects up or down and keep that order across launches.
- 🧹 **Clean up safely** — remove registrations in the TUI with an explicit confirmation step.
- 📦 **Ship one binary** — Go builds for macOS, Linux, and Windows.

## 🖥️ The TUI

![localhost-manager TUI with illustrative project data](docs/assets/localhost-manager-tui-demo.png)

*The image uses fictional project names, paths, ports, and logs.*

The compact workbench adapts to narrow and wide terminals. A restrained selection rail replaces the old block highlight; running, starting, unhealthy, stopped, and crashed projects remain distinguishable by both symbol and color. The detail pane keeps the command, URL, path, and latest output in view.

## ✨ Install with one prompt

Copy the prompt below into your local coding agent. It tells the agent to download localhost-manager, install the CLI and the bundled Skill, configure your PATH, and verify the complete setup.

```text
Install localhost-manager from https://github.com/huggon1/localhost-manager and complete the setup end to end.

1. Detect my operating system, CPU architecture, shell, and the tools already available. Use platform-appropriate commands and keep all installation paths user-owned when possible.
2. Ensure Go 1.25 or newer is available. If it is missing or too old, install or upgrade it with the established package manager for this machine. Ask before any step that requires administrator privileges.
3. Clone the localhost-manager repository into a stable user-owned source directory, or safely update an existing clean checkout. Preserve unrelated files and local changes. Do not create commits or push anything.
4. Build and install `github.com/huggon1/localhost-manager/cmd/lhm`. Put the `lhm` executable on my persistent PATH without overwriting unrelated shell configuration.
5. Install the repository's `skills/register-with-lhm` directory into the user-level Skill directory supported by this coding agent. Inspect the agent's local conventions or documentation instead of assuming a product-specific path. Copy the complete Skill directory and validate its `SKILL.md`. If this agent has no Skill mechanism, keep the repository-local Skill available and clearly report that limitation.
6. Open a fresh shell or reload its environment as needed. Verify that `command -v lhm` (or the platform equivalent) resolves the installed binary, `lhm --help` succeeds, and the installed Skill matches the repository copy.
7. Finish only after reporting the exact repository, binary, and Skill paths, plus any action I still need to take. Tell me that the only daily command I need to remember is: `lhm`.

Do not register or start any projects during installation unless I explicitly ask you to.
```

After that one-time setup, the only command you need to remember is:

```bash
lhm
```

Your coding agent will use the installed Skill to verify and register runnable projects as it works on them. localhost-manager opens the shared catalog from any directory.

<details>
<summary><strong>🛠️ Manual installation</strong></summary>

localhost-manager requires Go 1.25 or newer:

```bash
go install github.com/huggon1/localhost-manager/cmd/lhm@latest
lhm
```

To build from source:

```bash
git clone https://github.com/huggon1/localhost-manager.git
cd localhost-manager
make build
./dist/lhm
```

Copy `skills/register-with-lhm` into the user-level Skill directory documented by your coding agent.

</details>

## ➕ Register a project

localhost-manager accepts any command that starts a project with a known HTTP(S) URL. The command may use fixed ports, several internal services, or an optional `{port}` placeholder.

For a command that starts a web app on `4317` and an API health endpoint on `4318`:

```bash
lhm register \
  --path /absolute/path/to/atlas-web \
  --name atlas-web \
  --url 'http://127.0.0.1:4317' \
  --ready-url 'http://127.0.0.1:4318/health' \
  -- npm run dev
```

`--url` is what localhost-manager opens for the user. `--ready-url` is the endpoint localhost-manager waits for; it defaults to `--url`. Registration is idempotent, so registering the same canonical path updates the existing project.

Fixed-port projects are fully supported, but two processes still cannot bind the same fixed host port. localhost-manager reports that launch failure instead of rejecting the project at registration time.

When a project accepts an external port, `{port}` keeps collision avoidance automatic. It can appear in command arguments or environment values:

### More examples

```bash
# Vite / Next.js
lhm register --path . --name web -- npm run dev -- --port '{port}'

# Port through the environment
lhm register --path . --name web --env 'PORT={port}' -- npm run dev

# FastAPI
lhm register --path . --name api -- uvicorn main:app --port '{port}'

# Python static server
lhm register --path . --name docs -- python3 -m http.server '{port}'
```

Pass ordinary, non-secret environment settings with repeated `--env KEY=VALUE` flags. Keep credentials in the project's existing secret mechanism.

## ⌨️ Keyboard shortcuts

| Key | Action |
|:---:|---|
| `↑` / `k` | Select previous project |
| `↓` / `j` | Select next project |
| `Space` | Pick up the selected project; while picked up, `↑` / `k` and `↓` / `j` move it and persist the order |
| `Space` / `Enter` / `Esc` | Put the project down and exit reorder mode |
| `Enter` | Start a stopped project or open a running project |
| `o` | Open the selected project's URL |
| `x` | Stop the selected project |
| `r` | Restart the selected project |
| `l` | Toggle expanded logs |
| `d` | Remove a project after confirmation; running projects are stopped first |
| `/` | Search projects |
| `q` | Quit localhost-manager without stopping projects |

## 🧰 CLI

The same core powers the TUI and non-interactive commands, so humans and agents always see the same state.

| Command | Purpose |
|---|---|
| `lhm` | Open the TUI |
| `lhm register …` | Create or update a project |
| `lhm list [--json]` | List every project |
| `lhm start <project>` | Start and wait until the readiness URL responds |
| `lhm stop <project>` | Stop the complete process group |
| `lhm restart <project>` | Restart a project |
| `lhm status <project>` | Print machine-readable state |
| `lhm logs [-n lines] <project>` | Print recent logs |
| `lhm remove <project>` | Stop and remove a project |

A project can be addressed by its display name or stable ID. If a name is ambiguous, localhost-manager asks for the ID instead of guessing.

## 🤖 Agent-native registration

The one-prompt setup installs the repository-local [`register-with-lhm`](skills/register-with-lhm/SKILL.md) Skill. It teaches compatible coding agents to register the command they actually verified—not one inferred from filenames.

```text
skills/register-with-lhm/SKILL.md
```

Agents that support Agent Skills or repository-local skills can use this file directly. The installation prompt asks each agent to discover and use its own supported Skill location instead of assuming a product-specific directory.

The Skill guides the agent to:

1. 🔍 Find the intended development command and primary URL.
2. 🧪 Run the command as-is, using `{port}` only when the project supports it.
3. ✅ Verify the primary URL or a separate HTTP health endpoint.
4. 🧹 Stop the validation process.
5. 📝 Register the verified command, URL, and readiness URL.

The Skill only maintains registration. localhost-manager remains the single source of truth for ports, processes, state, and logs.

## 🧭 How it works

```mermaid
flowchart LR
    Agent["🤖 Coding agent"] --> Skill["🪄 register-with-lhm"]
    Skill --> CLI["⌨️ localhost-manager CLI"]
    Human["👤 Developer"] --> TUI["🖥️ localhost-manager TUI"]
    CLI --> Manager["🧠 Manager"]
    TUI --> Manager
    Manager --> Registry["🗃️ SQLite registry"]
    Manager --> Ports["🚦 Port allocator"]
    Manager --> Processes["🧵 Process supervisor"]
    Manager --> Logs["📜 Log files"]
```

The TUI and CLI cross one small manager interface. Platform-specific behavior stays in dedicated Unix and Windows process adapters, keeping the rest of the code portable and testable.

## 💾 Data and safety

localhost-manager stores its registry and logs in the operating system's user configuration directory:

- macOS: `~/Library/Application Support/localhost-manager/`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/localhost-manager/`
- Windows: `%AppData%\localhost-manager\`

Set `LHM_HOME` to use an isolated location.

Commands are stored and executed as argument arrays, not interpolated shell strings. On Unix, projects run in independent sessions; on Windows, localhost-manager controls the spawned process tree with the platform process tools. Quitting the TUI leaves running projects alive.

## 🛠️ Development

```bash
make test          # unit, integration, process lifecycle, and layout tests
make build         # current platform
make cross-build   # macOS, Linux, and Windows release binaries
```

The integration suite starts a real temporary HTTP server, verifies its assigned port, stops its process group, and confirms the final state. TUI tests also guard 80×24 and 120×36 layouts against overflow.

## 🗺️ Direction

- 📚 Project groups and favorites
- ❤️ TCP and log-based readiness probes
- 🔌 Multiple named dynamic ports
- 🔎 Runtime URL discovery from logs
- 💤 Start on request and idle shutdown
- 🍺 Homebrew and additional package managers
- 🪟 Stronger native Windows process supervision

---

<div align="center">

Built for developers—and agents—who always have one more local project running. ✨

</div>
