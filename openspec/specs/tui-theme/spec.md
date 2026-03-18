## ADDED Requirements

### Requirement: Color Palette

The theme SHALL define a color palette with a dark background, purple accent colors, and semantic colors for priority levels and sync states.

#### Scenario: Base colors

- **WHEN** the theme is loaded
- **THEN** it SHALL define a dark background color, a primary text color with high contrast against the background, a secondary text color for less prominent elements, and a tertiary text color for subtle/disabled elements

#### Scenario: Accent colors

- **WHEN** the theme is loaded
- **THEN** it SHALL define a purple primary accent color (`#7F77DD`) for selected elements, active column headers, and the application title, and a lighter purple variant (`#AFA9EC`) for accent borders

#### Scenario: Priority colors

- **WHEN** the theme is loaded
- **THEN** it SHALL define colors for card priority borders: red/orange (`#FAECE7` background, `#993C1D` text) for high, yellow/amber (`#FAEEDA` background, `#854F0B` text) for medium, green (`#E1F5EE` background, `#0F6E56` text) for low, and grey (`#B4B2A9`) for unset

#### Scenario: Sync state colors

- **WHEN** the theme is loaded
- **THEN** it SHALL define indicator colors: green (`#1D9E75`) for synced, yellow for syncing, red for sync error, and grey for offline

### Requirement: Column-Specific Border Colors

Each column SHALL have a distinct left-border color for its cards to provide visual lane identity.

#### Scenario: Column border colors

- **WHEN** cards are rendered in a column
- **THEN** the card left border color SHALL be determined by the column: grey (`#B4B2A9`) for Backlog, blue (`#85B7EB`) for Ready, purple (`#7F77DD`) for Doing, teal (`#5DCAA5`) for Review, and green (`#97C459`) for Done

### Requirement: Lipgloss Style Definitions

The theme package SHALL export pre-configured Lipgloss styles for all visual components.

#### Scenario: Card styles

- **WHEN** a card component requests styles from the theme
- **THEN** the theme SHALL provide a base card style with background color, border radius, and padding, a selected card style with the accent border and highlighted background (`#EEEDFE`), and priority badge styles for each priority level

#### Scenario: Column header styles

- **WHEN** a column header is rendered
- **THEN** the theme SHALL provide a default header style with uppercase text, letter spacing, and secondary text color, and an active header style using the accent color

#### Scenario: Status bar styles

- **WHEN** the status bar is rendered
- **THEN** the theme SHALL provide a status bar container style with a top border, and key hint styles with distinct formatting for the key binding versus the action label

### Requirement: Done Column Styling

Cards in the Done column SHALL have a visually muted appearance.

#### Scenario: Done card rendering

- **WHEN** a card is in the Done column
- **THEN** the card SHALL be rendered with reduced opacity (muted text colors) and the summary text SHALL use a strikethrough style

### Requirement: Terminal Color Profile Compatibility

The theme SHALL use hex color values and rely on Lipgloss automatic color profile detection for degradation.

#### Scenario: TrueColor terminal

- **WHEN** the terminal supports TrueColor
- **THEN** the theme SHALL render with full hex color accuracy

#### Scenario: Limited color terminal

- **WHEN** the terminal supports only 256 or 16 colors
- **THEN** Lipgloss SHALL automatically degrade the hex colors to the nearest available color, and the theme SHALL remain usable without manual fallback configuration
