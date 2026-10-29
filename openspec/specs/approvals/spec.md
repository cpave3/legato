## ADDED Requirements

### Requirement: Durable approval requests
Legato SHALL keep each request that needs the person as an approval with an ID of the form `ap-` followed by 8 hexadecimal characters, a source, a source key, a task, an execution, a title, details, options and a status. Submitting a request with a source and source key that already exist SHALL return the existing approval and SHALL NOT create a second one.

#### Scenario: New request
- **WHEN** a source submits a request that does not exist
- **THEN** an approval SHALL be stored with status `pending`
- **AND** `EventApprovalChanged` SHALL be published with kind `created`

#### Scenario: Same request again
- **WHEN** a source submits the same source and source key a second time
- **THEN** the existing approval SHALL be returned
- **AND** the result SHALL report that nothing was created

### Requirement: The first decision wins
Deciding an approval SHALL succeed only while the approval is `pending`. A decision SHALL be applied to its source through the resolver that the source registered.

#### Scenario: Two decisions
- **WHEN** two decisions are made on one pending approval
- **THEN** the first SHALL be applied
- **AND** the second SHALL fail with a conflict and SHALL NOT reach the source

#### Scenario: Resolver fails
- **WHEN** the source's resolver returns an error
- **THEN** the approval SHALL have status `failed` and keep the error

#### Scenario: Unknown option
- **WHEN** a decision names an option the approval does not have
- **THEN** the decision SHALL be refused and the approval SHALL stay `pending`

### Requirement: Agent permission and question requests become approvals
Legato SHALL follow the event stream of every running agent that has a native endpoint, in the TUI and in `legato serve`, whether or not a web server runs. Each `permission_request` and `question_request` SHALL become an approval with source `agent_permission` or `agent_question` and source key `<execution>/<session>/<requestId>`. When a stream opens, the pending permissions and questions of the session's snapshot SHALL be submitted.

#### Scenario: Permission request
- **WHEN** a native agent emits `permission_request` with a command and options
- **THEN** a pending approval SHALL hold the tool, the command, the reason and the options
- **AND** a `deny` option SHALL be present

#### Scenario: Request raised before Legato started
- **WHEN** Legato starts while an agent has a pending permission
- **THEN** that permission SHALL become an approval when the agent's stream opens

#### Scenario: Approve a permission
- **WHEN** the person approves an agent permission with option `once`
- **THEN** Legato SHALL call `ResolvePermission` for the agent's task with decision `allow` and option `once`

#### Scenario: Answer a question
- **WHEN** the person answers an agent question
- **THEN** Legato SHALL call `AnswerQuestion` with the answers given

### Requirement: Approvals close when their request does
A pending approval SHALL become `withdrawn` when the agent reports its request resolved or timed out, when its run finishes, or when a fresh snapshot no longer lists it. It SHALL become `expired` when its agent ends. A dismissed agent permission SHALL be denied with feedback that says it was dismissed in Legato, and a dismissed question SHALL be dismissed.

#### Scenario: Answered in the terminal
- **WHEN** an agent emits `permission_resolved` for a request whose approval is pending
- **THEN** the approval SHALL be `withdrawn`

#### Scenario: Agent ends
- **WHEN** `EventAgentDied` is published for a task with pending approvals
- **THEN** those approvals SHALL be `expired`

#### Scenario: Dismissed
- **WHEN** the person dismisses a pending agent permission
- **THEN** Legato SHALL deny it with feedback "Dismissed in Legato"
- **AND** the approval SHALL be `dismissed`

### Requirement: Approvals API
The web server SHALL serve `GET /api/approvals` (filter by `status` and `task`), `GET /api/approvals/{id}`, `POST /api/approvals/{id}/decision` and `POST /api/approvals/{id}/dismiss`, behind the web token, and SHALL send `approvals_changed` to WebSocket clients when an approval changes.

#### Scenario: Decision conflict
- **WHEN** a client posts a decision for an approval that is no longer pending
- **THEN** the response SHALL be `409`

#### Scenario: Pending list
- **WHEN** a client asks for `status=pending`
- **THEN** only pending approvals SHALL be returned, oldest first

### Requirement: Approvals in Attention
Each pending approval SHALL be an Attention item of producer `approvals` and signal kind `approval` in Needs intervention, with one execute action for each option it can take without more input. Executing an action SHALL decide the approval at the item's revision.

#### Scenario: Approve from Attention
- **WHEN** the person executes an approval's `allow` action in Attention
- **THEN** the approval SHALL be decided with that option
- **AND** the Attention item SHALL be resolved

### Requirement: Web tray
Every page of the web UI SHALL show a tray while at least one approval is pending, with each approval's title, details and options. A page address with `?approval=<id>` SHALL open the tray on that approval.

#### Scenario: Request arrives
- **WHEN** an approval is created while a page is open
- **THEN** the tray SHALL show it without a reload

### Requirement: Phone notification
When `notifications.approvals` is true and ntfy is configured, the process that created an approval SHALL send one ntfy notification for it. The notification SHALL carry no secret. When the server's address is known, its click link SHALL be `legato://approval?url=<server>&id=<id>`. The Android app SHALL open that server's page at `/?approval=<id>`.

#### Scenario: One notification per approval
- **WHEN** two Legato processes see the same new agent request
- **THEN** one notification SHALL be sent

### Requirement: Approvals in the TUI
While approvals are pending, the TUI SHALL show how many on every view, and a key SHALL open an overlay that answers them one at a time: `y` takes the approval's first option and `n` denies it.

#### Scenario: Deny from the TUI
- **WHEN** the person presses `n` on an approval in the overlay
- **THEN** the approval SHALL be denied
