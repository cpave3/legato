## MODIFIED Requirements

### Requirement: Card Rendering

Each card SHALL display the task ID, a truncated title, agent status with duration (when applicable), and visual indicators for priority. Cards with agent data SHALL be taller than cards without.

#### Scenario: Card content display — no agent

- **WHEN** a card is rendered that has no active agent and no duration history
- **THEN** it SHALL show the provider icon and task ID on the first line, the title truncated to fit on the second line, and priority/issue type metadata on the third line

#### Scenario: Card content display — with agent

- **WHEN** a card is rendered that has an active agent or has duration history
- **THEN** it SHALL show the provider icon and task ID on the first line, the title on the second line, the agent status with duration on the third line, and priority/issue type metadata on the fourth line

#### Scenario: Agent status line rendering

- **WHEN** the agent status line is rendered for a card with an active agent
- **THEN** it SHALL display the agent state icon, the state label (RUNNING/WAITING/IDLE), and the cumulative duration for the current state formatted as a human-readable string (e.g., "2h 15m")

#### Scenario: Agent duration display for inactive agent with history

- **WHEN** a card has no active agent but has accumulated duration history
- **THEN** the agent line SHALL display the total working and waiting durations (e.g., "1h 30m working · 20m waiting")

#### Scenario: Priority indicator

- **WHEN** a card has a priority value
- **THEN** the card SHALL display a colored left border matching the priority: red/orange for high, yellow for medium, green for low, and grey for unset

#### Scenario: Title truncation

- **WHEN** a card title exceeds the available column width minus padding
- **THEN** the title SHALL be truncated with an ellipsis to fit within the available space

#### Scenario: Warning indicator placement

- **WHEN** a card has a warning flag set
- **THEN** the warning icon SHALL be displayed on the task ID line after the provider icon, before the key

## ADDED Requirements

### Requirement: Duration data on CardData

The `CardData` struct SHALL include fields for aggregated state durations so the board can render them without additional queries.

#### Scenario: CardData population during data load

- **WHEN** the board loads data via `DataLoadedMsg`
- **THEN** the app SHALL query `GetStateDurationsBatch` for all visible task IDs and populate `CardData.WorkingDuration` and `CardData.WaitingDuration` fields

#### Scenario: CardData with no duration data

- **WHEN** a task has no state intervals
- **THEN** `CardData.WorkingDuration` and `CardData.WaitingDuration` SHALL be zero-value `time.Duration`

### Requirement: Human-readable duration formatting

The board SHALL format durations as concise human-readable strings.

#### Scenario: Duration under one hour

- **WHEN** a duration is less than 60 minutes
- **THEN** it SHALL be formatted as `Xm` (e.g., "45m")

#### Scenario: Duration over one hour

- **WHEN** a duration is 60 minutes or more
- **THEN** it SHALL be formatted as `Xh Ym` (e.g., "2h 15m")

#### Scenario: Duration under one minute

- **WHEN** a duration is less than 60 seconds
- **THEN** it SHALL be formatted as `<1m`

#### Scenario: Zero duration

- **WHEN** a duration is zero
- **THEN** it SHALL not be displayed (the label for that state is omitted)

### Requirement: Uniform card height within columns

Cards within a single column SHALL have uniform height to prevent visual jitter.

#### Scenario: Mixed agent and non-agent cards in a column

- **WHEN** a column contains both cards with agent data and cards without
- **THEN** all cards in that column SHALL be rendered at the height of the tallest card, with shorter cards padded with empty lines
