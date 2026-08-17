---
name: register-with-lhm
description: Verify and register runnable local projects with localhost-manager (lhm). Use after creating, modifying, or validating any local app, API, documentation server, or multi-process development command that starts from one command and exposes an HTTP(S) URL.
---

# Register with lhm

Keep localhost-manager's global project catalog aligned with the command that actually works, regardless of which coding agent is doing the work.

1. Check for `lhm` with `command -v lhm`. If unavailable, finish the requested project work and report that automatic registration needs localhost-manager installed.
2. Determine the intended development command, the primary URL a user should open, and the HTTP URL that proves the project is ready. Use the primary URL for readiness when no separate health endpoint exists.
3. Run the command once and verify the readiness URL. Preserve the command as-is when it uses fixed ports. When it accepts an external port through an argument or environment variable, verify it with an available port and replace that concrete value with `{port}`. Stop the validation process before continuing.
4. Register the canonical project root from any directory. Preserve argument separators such as npm's `--`.

   Fixed URL or multi-process command:

   ```bash
   lhm register --path "/absolute/project/root" --name "project-name" --url "http://127.0.0.1:4317" --ready-url "http://127.0.0.1:4318/health" -- command arg
   ```

   Dynamic port through an argument or environment variable:

   ```bash
   lhm register --path "/absolute/project/root" --name "project-name" --url 'http://localhost:{port}' -- command --port '{port}'
   lhm register --path "/absolute/project/root" --name "project-name" --env 'PORT={port}' -- command
   ```

5. Complete registration only when `lhm list --json` contains the updated path, command, primary URL, and readiness URL. Run `lhm status project-name` only when the project is already meant to remain running.

Pass ordinary non-secret environment settings with repeated `--env KEY=VALUE`. Keep credentials in the project's existing secret mechanism and out of the localhost-manager registry.

Choose one primary URL when a command exposes several services. Register verified fixed-port commands instead of modifying the project solely for localhost-manager; report that fixed ports can still conflict with other processes. If the final URL cannot be known until after launch, report that localhost-manager does not yet support runtime URL discovery instead of recording a guessed URL.
