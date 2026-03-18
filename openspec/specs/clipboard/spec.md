## ADDED Requirements

### Requirement: Clipboard Platform Detection

The clipboard package MUST detect the available clipboard tool at startup based on the operating system and display environment. Detection MUST follow this priority order:

1. macOS: use `pbcopy`
2. Linux with `$WAYLAND_DISPLAY` set: look for `wl-copy`
3. Linux with `$DISPLAY` set: look for `xclip`, then fall back to `xsel`

The package MUST expose an `Available() bool` method to check whether a clipboard tool was found.

#### Scenario: macOS detection

- **WHEN** the application starts on macOS (`runtime.GOOS == "darwin"`)
- **THEN** the clipboard package selects `pbcopy` as the clipboard tool and `Available()` returns true

#### Scenario: Linux Wayland detection

- **WHEN** the application starts on Linux with `$WAYLAND_DISPLAY` set and `wl-copy` is in `$PATH`
- **THEN** the clipboard package selects `wl-copy` as the clipboard tool and `Available()` returns true

#### Scenario: Linux X11 detection with xclip

- **WHEN** the application starts on Linux with `$DISPLAY` set and `xclip` is in `$PATH`
- **THEN** the clipboard package selects `xclip -selection clipboard` as the clipboard tool and `Available()` returns true

#### Scenario: Linux X11 fallback to xsel

- **WHEN** the application starts on Linux with `$DISPLAY` set, `xclip` is not found, but `xsel` is in `$PATH`
- **THEN** the clipboard package selects `xsel --clipboard --input` as the clipboard tool and `Available()` returns true

#### Scenario: No clipboard tool found

- **WHEN** no supported clipboard tool is found on the system
- **THEN** `Available()` returns false and the clipboard package logs a warning

### Requirement: Startup Clipboard Warning

The application MUST display a non-blocking warning in the status bar at startup if no clipboard tool is detected.

#### Scenario: Warning displayed when clipboard unavailable

- **WHEN** the TUI initializes and `clipboard.Available()` returns false
- **THEN** the status bar displays a warning message such as "clipboard unavailable -- install xclip or wl-copy"

#### Scenario: No warning when clipboard is available

- **WHEN** the TUI initializes and `clipboard.Available()` returns true
- **THEN** no clipboard warning is shown in the status bar

### Requirement: Copy to Clipboard

The clipboard package MUST expose a `Copy(text string) error` function that writes the provided text to the system clipboard using the detected tool via `exec.Command`, piping content to the tool's stdin.

#### Scenario: Successful copy

- **WHEN** `Copy()` is called with a non-empty string and a clipboard tool is available
- **THEN** the text is piped to the clipboard tool's stdin and the function returns nil

#### Scenario: Copy when no clipboard tool available

- **WHEN** `Copy()` is called but no clipboard tool was detected
- **THEN** the function returns an error indicating clipboard is not available

#### Scenario: Clipboard tool execution fails

- **WHEN** `Copy()` is called and the clipboard tool process exits with a non-zero status
- **THEN** the function returns an error wrapping the tool's stderr output

### Requirement: Description-Only Copy Format

When the user presses `y` in the detail view, the application MUST copy the ticket description in a description-only format. This format MUST be produced by `BoardService.ExportCardContext()` with the description-only export format.

The description-only format MUST contain:
- A heading with the ticket key and summary (e.g., `## REX-1238: Refactor user service`)
- The full markdown description

#### Scenario: Copy description with y

- **WHEN** the user presses `y` in the detail view
- **THEN** the application calls `ExportCardContext()` with the description-only format, copies the result to the clipboard, and shows a confirmation in the status bar

#### Scenario: Copy description when clipboard unavailable

- **WHEN** the user presses `y` but no clipboard tool is available
- **THEN** the status bar displays an error message indicating clipboard is not available

### Requirement: Full Context Copy Format

When the user presses `Y` in the detail view, the application MUST copy a full structured context block. This format MUST be produced by `BoardService.ExportCardContext()` with the full-context export format.

The full context format MUST contain:
- A top-level heading with the ticket key (e.g., `# Ticket: REX-1238`)
- Metadata fields: summary, type, priority, epic, labels, URL
- A horizontal rule separator
- The full markdown description

#### Scenario: Copy full context with Y

- **WHEN** the user presses `Y` in the detail view
- **THEN** the application calls `ExportCardContext()` with the full-context format, copies the result to the clipboard, and shows a confirmation in the status bar

#### Scenario: Copy full context when clipboard unavailable

- **WHEN** the user presses `Y` but no clipboard tool is available
- **THEN** the status bar displays an error message indicating clipboard is not available
