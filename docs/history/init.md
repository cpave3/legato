# Legato — Go project bootstrap

## Prerequisites

- Go 1.23+ installed (`brew install go`)
- Git configured
- `$GOPATH/bin` on your `$PATH`

## 1. Init the module

```bash
mkdir legato && cd legato
git init
go mod init github.com/cpave3/legato
```

## 2. Create the directory structure

```bash
# Entry point
mkdir -p cmd/legato

# Engine layer (infrastructure, no business logic)
mkdir -p internal/engine/store
mkdir -p internal/engine/jira
mkdir -p internal/engine/events

# Service layer (business logic, presentation-agnostic)
mkdir -p internal/service

# TUI presentation layer
mkdir -p internal/tui/board
mkdir -p internal/tui/detail
mkdir -p internal/tui/overlay
mkdir -p internal/tui/statusbar
mkdir -p internal/tui/theme
mkdir -p internal/tui/clipboard

# Future: HTTP/WS server
mkdir -p internal/server

# Config
mkdir -p config
```

## 3. Install dependencies

```bash
# TUI framework
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
go get github.com/charmbracelet/glamour
go get github.com/charmbracelet/bubbles

# SQLite (pure Go, no CGO)
go get modernc.org/sqlite
go get github.com/jmoiron/sqlx

# Config
go get gopkg.in/yaml.v3

# Run go mod tidy to clean up
go mod tidy
```

## 4. Verify it builds

Create a minimal `cmd/legato/main.go`:

```go
package main

import (
 "fmt"
 "os"

 tea "github.com/charmbracelet/bubbletea"
)

type model struct{}

func (m model) Init() tea.Cmd { return nil }
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
 switch msg := msg.(type) {
 case tea.KeyMsg:
  if msg.String() == "q" {
   return m, tea.Quit
  }
 }
 return m, nil
}
func (m model) View() string { return "legato v0.1.0 — press q to quit\n" }

func main() {
 p := tea.NewProgram(model{})
 if _, err := p.Run(); err != nil {
  fmt.Fprintf(os.Stderr, "error: %v\n", err)
  os.Exit(1)
 }
}
```

```bash
go run ./cmd/legato
```

If you see the message and can quit with `q`, the project is bootstrapped.

## 5. Build and install

```bash
# Build binary
go build -o bin/legato ./cmd/legato

# Or install to $GOPATH/bin
go install ./cmd/legato
```

## 6. Project conventions

**Layer boundaries are enforced by import rules:**

- `internal/engine/` — imports only stdlib and third-party libs. Never imports `service/` or `tui/`.
- `internal/service/` — imports `engine/`. Never imports `tui/` or anything from `bubbletea`/`lipgloss`/`glamour`.
- `internal/tui/` — imports `service/` (via interfaces). This is the only layer that touches `bubbletea`, `lipgloss`, `glamour`.
- `cmd/legato/` — wires everything together. Creates engine instances, passes them to services, passes services to TUI.

**Testing:**

```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/engine/store/...

# With verbose output
go test -v ./...
```

**Linting (optional but recommended):**

```bash
# Install golangci-lint
brew install golangci-lint

# Run
golangci-lint run ./...
```

## 7. .gitignore

```
bin/
*.db
*.sqlite
.DS_Store
```
