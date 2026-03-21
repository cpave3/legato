## 1. Config — Editor Resolution

- [ ] 1.1 Add `Editor string` field to config struct with `yaml:"editor"` tag
- [ ] 1.2 Add `ResolveEditor()` function in config package: config value → `$VISUAL` → `$EDITOR` → `vi`
- [ ] 1.3 Tests for `ResolveEditor` covering all four precedence levels

## 2. Service Layer — CreateTask Signature + UpdateTaskDescription

- [ ] 2.1 Add `description` parameter to `BoardService.CreateTask` interface method (title, description, column, priority)
- [ ] 2.2 Update `boardService.CreateTask` implementation to set `Description` and `DescriptionMD` on the store task
- [ ] 2.3 Update all `BoardService` test fakes (app_test, board/model_test, detail/model_test, server_test, fakeservice) to match new signature
- [ ] 2.4 Add `UpdateTaskDescription(ctx, id, description string) error` to `BoardService` interface
- [ ] 2.5 Implement `UpdateTaskDescription` on `boardService`: load task, reject if provider non-nil, set description/description_md, persist, publish event
- [ ] 2.6 Tests for `UpdateTaskDescription`: success, remote-task rejection, not-found

## 3. Create Overlay — Description Field + Focus Cycling

- [ ] 3.1 Fix space input: handle `tea.KeySpace` in addition to `tea.KeyRunes` so titles (and descriptions) can contain spaces
- [ ] 3.2 Add `description` field and `focusField` enum (`focusTitle`/`focusDescription`) to `CreateOverlay`
- [ ] 3.3 Update `tab` handling: cycle title → column → description → title
- [ ] 3.4 Route character input, space, and backspace to the focused field (title or description)
- [ ] 3.4 Support `ctrl+j` for newline insertion when description is focused
- [ ] 3.5 Include `Description` field in `CreateTaskMsg` and populate on submit
- [ ] 3.6 Update `View()` to render description field with focus indicator and cursor
- [ ] 3.7 Update hint line to show `ctrl+j newline` when description focused
- [ ] 3.8 Tests: tab cycling, typing in each field, submit includes description, ctrl+j newline

## 4. App Wiring — CreateTask Call Update

- [ ] 4.1 Update `handleCreateTask` in `app.go` to pass `msg.Description` to `BoardService.CreateTask`

## 5. Detail View — Editor-Based Description Editing

- [ ] 5.1 Add `e` key handler in detail model: check local task, write description_md to temp file, exec editor via `tea.ExecProcess`
- [ ] 5.2 Add message types: `EditDescriptionMsg` (carries temp file path), `DescriptionEditedMsg` (carries new content or error)
- [ ] 5.3 On editor return: read temp file, call `UpdateTaskDescription`, refresh card, re-render content
- [ ] 5.4 Handle editor error: show feedback, clean up temp file
- [ ] 5.5 Show "Cannot edit remote task description" feedback when `e` pressed on remote task
- [ ] 5.6 Add `e edit` to status bar hints (conditional on local task)
- [ ] 5.7 Wire editor config: detail model needs editor command string (passed from app or resolved at exec time)
- [ ] 5.8 Tests: `e` key on local task produces exec cmd, `e` on remote task shows feedback, status bar hints conditional
