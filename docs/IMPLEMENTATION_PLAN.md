# revboard — Implementation Plan

Companion of [SPEC.md](SPEC.md). Each phase ends with a verifiable outcome; phases 1–4 are pure
Go and can be developed without Node.js thanks to a placeholder UI.

## Phase 0 — Skeleton
- `go.mod` (module `revboard`, Go 1.26+, the minimum required by `modernc.org/sqlite`), dependencies: `github.com/spf13/cobra`, `modernc.org/sqlite`.
- `main.go`, `internal/cli/root.go` with `--db`, `--project`, `version`.
- `internal/webui/embed.go` + `placeholder/index.html` (copied to `dist/browser` by `make placeholder`) so `go build` works before the UI exists.
- `Makefile`: `web-install`, `web`, `build`, `test`, `dev-api`, `dev-web`, `install`, `clean`.
- **Done when**: `go build ./... && ./revboard --help` works.

## Phase 1 — Diff parser (`internal/diff`)
- AST types, line-oriented state machine, C-quoted path unquoting, `ParseHunkBody`, `Hunk.Body()`.
- Table-driven tests listed in SPEC §13.
- **Done when**: `go test ./internal/diff` green, including a real `git diff` fixture.

## Phase 2 — Git runner (`internal/gitx`)
- `SplitCommand` (shell words), `BuildArgs` (validation + forced flags), `RepoRoot`, `HeadSHA`, `Run`.
- **Done when**: unit tests cover splitting and every refused flag.

## Phase 3 — Store (`internal/store`)
- Config path resolution (`internal/config`), open with pragmas, embedded migrations.
- Projects (upsert/unique names, current project), sessions, files/hunks bulk insert in a TX,
  status cascade/recompute, progress aggregation (single SQL with `SUM(CASE…)`), comments, pins,
  ui_state, `ReplaceSnapshot` with carry-over.
- **Done when**: store tests (temp DB) pass.

## Phase 4 — Use-cases + CLI + server
- `internal/review`: `Start(ctx, workDir, args, name)`, `Refresh(ctx, sessionID)`.
- `internal/server`: routes (Go 1.22 `ServeMux` patterns), guards, JSON helpers, SPA fallback.
- `internal/browser`: `xdg-open` / `open` / `rundll32`, WSL detection (`wslview`, `explorer.exe`), `REVBOARD_BROWSER` override.
- CLI commands: `review start|resume|list|refresh|archive|delete`, `project list|switch|rename|remove`, `status`, `serve`.
- Graceful shutdown on SIGINT/SIGTERM.
- **Done when**: e2e test with a temp git repo and `httptest` passes; manual `curl` against `revboard review start --no-open`.

## Phase 5 — Angular SPA (`web/`)
- `ng new` (standalone, zoneless, SCSS, no SSR), PrimeNG 22 + Aura theme + PrimeIcons.
- `angular.json` output path → `../internal/webui/dist`; `proxy.conf.json` for `ng serve` → Go API.
- `core/`: models, `ApiService` (+ `X-GRB-Client` interceptor), `ReviewStore`, tree builder (keyboard shortcuts are handled by `ReviewPage`).
- `features/dashboard`: projects & sessions tables.
- `features/review`: toolbar, file tree (CDK virtual scroll + pinned), file diff, hunk, hunk lines, note thread, shortcuts dialog.
- **Done when**: `ng build` succeeds, the bundle is embedded, and a session can be reviewed end-to-end in the browser.

## Phase 6 — Hardening & docs
- Large diff smoke test (generated 2 000-file diff): load time, tree, status changes.
- README (install, build, usage, shortcuts, dev workflow), `--help` texts with examples.
- **Done when**: `make build` produces a single `revboard` binary that runs without any external file
  except `git`.

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| Exotic git output (quoted paths, binary, renames) | Force normalising flags; parser fixtures generated from real git. |
| Very large diffs in the browser | Lazy file bodies, `content-visibility`, fine-grained signals, tree virtual scroll. |
| Local API reachable by other web pages | Loopback bind + Host check + mandatory custom header (no CORS). |
| `go:embed` requires built assets | Placeholder page committed; `make build` always builds the UI first, `make placeholder` for Go-only work. |
| PrimeNG Tree API changed in v22 and is heavy for 5 000 nodes | Custom flattened tree with CDK virtual scroll (SPEC §11.2). |
