Act as a Senior Technical Product Manager and Software Architect. Write a comprehensive Software Requirements Specification (SRS) and Technical Design Document for a local developer tool called "Git-Review-Board" (GRB).

# Context
I am a developer who performs large code reviews across multiple repositories. Standard terminal tools (`git diff`, `delta`) lack session persistence and spatial navigation for large changesets. I need a single-binary CLI tool that captures a git diff, opens a local web application to track my code review progress, and stores my reviews centrally so I can easily switch between different projects.

# Core User Flow
1. **Multi-Project Management:** The user can view active reviews across projects using `grb project list` and switch contexts using `grb project switch <project-name>`.
2. **Review Initialization:** The user runs a command like: `grb review start "git diff dev feat/oai-article-16 -- ':(exclude)*.md'"`
3. **Execution:** The Go CLI executes the git command, parses the unified diff, saves the state to SQLite, and spins up a local web server.
4. **UI Interaction:** The user's browser opens. The web app is served directly from the Go binary via `go:embed`. The user navigates a file tree, views diffs, marks hunks/files as "Reviewed" or "Needs Attention", and attaches private mental notes.
5. **Session Resumption:** The user closes the browser. Days later, they run `grb review resume`, and the tool restores their exact state (progress, notes, pins) for that specific session.

# Technology Stack Constraints
- **Backend & CLI:** Go (Golang). Must compile to a single binary. Use a framework like Cobra for CLI commands.
- **Frontend:** Angular (using Signals for state management and RxJS), PrimeNG for UI components. The built SPA must be embedded in the Go binary using `go:embed`.
- **Database:** SQLite. Stored centrally in the user's home directory (e.g., `~/.config/grb/grb.db`) to enable tracking and switching between multiple repositories and review sessions.
- **Code Rules:** All code comments generated in the specification, SQL schemas, or future code implementations MUST be in English.

# Required Deliverables in the Output
1. **System Architecture Diagram (Mermaid.js):** Show the interaction between the Go CLI, Embedded Angular SPA, SQLite DB, and Git CLI.
2. **CLI Command Structure:** Define the CLI API (e.g., commands for `project`, `review`, `status`).
3. **Database Schema:** Provide the SQLite schema for tracking Projects, Review Sessions, File Statuses, and Line Comments.
4. **Data Models:** Define the Go Structs and TypeScript interfaces representing the Parsed Diff AST and UI state.
5. **API Endpoints:** Define the local REST API exposed by the Go server to sync state with the Angular frontend.
6. **State Management Strategy:** Explain how Angular Signals will efficiently handle DOM updates when a user marks a 1,000-line file as "reviewed".

Ensure the specification is highly technical, pragmatic, and ready to be handed to a developer for immediate implementation.
