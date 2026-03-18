## ADDED Requirements

### Requirement: Config File Path Resolution

The config parser SHALL resolve the configuration file path using the following precedence:
1. If the `$LEGATO_CONFIG` environment variable is set, use its value as the file path.
2. If the `$XDG_CONFIG_HOME` environment variable is set, use `$XDG_CONFIG_HOME/legato/config.yaml`.
3. Otherwise, use `~/.config/legato/config.yaml`.

#### Scenario: LEGATO_CONFIG overrides all other paths
- **WHEN** `$LEGATO_CONFIG` is set to `/tmp/my-legato.yaml`
- **THEN** the parser SHALL attempt to load the config from `/tmp/my-legato.yaml`

#### Scenario: XDG_CONFIG_HOME is used when LEGATO_CONFIG is unset
- **WHEN** `$LEGATO_CONFIG` is not set and `$XDG_CONFIG_HOME` is set to `/home/user/.myconfig`
- **THEN** the parser SHALL attempt to load the config from `/home/user/.myconfig/legato/config.yaml`

#### Scenario: Default path is used when no env vars are set
- **WHEN** neither `$LEGATO_CONFIG` nor `$XDG_CONFIG_HOME` is set
- **THEN** the parser SHALL attempt to load the config from `~/.config/legato/config.yaml`

### Requirement: YAML Parsing

The config parser SHALL parse the configuration file as YAML using `gopkg.in/yaml.v3`. The parser SHALL unmarshal the YAML into a typed Go struct that represents the full configuration schema including `jira`, `board`, `theme`, and `keybindings` sections.

#### Scenario: Valid config file is parsed
- **WHEN** a well-formed YAML config file exists at the resolved path
- **THEN** the parser SHALL return a populated config struct with all fields set according to the file contents

#### Scenario: Malformed YAML
- **WHEN** the config file contains invalid YAML syntax
- **THEN** the parser SHALL return an error describing the parse failure

### Requirement: Environment Variable Expansion

The config parser SHALL expand environment variable references in the YAML file before parsing. Expansion SHALL use `os.ExpandEnv` on the raw file bytes so that `${VAR_NAME}` and `$VAR_NAME` syntax is supported. Expansion SHALL occur before YAML unmarshalling.

#### Scenario: Environment variable in config value
- **WHEN** the config file contains `api_token: "${LEGATO_JIRA_TOKEN}"` and `$LEGATO_JIRA_TOKEN` is set to `secret123`
- **THEN** the parsed config struct SHALL have `Jira.APIToken` set to `secret123`

#### Scenario: Unset environment variable expands to empty string
- **WHEN** the config file contains `api_token: "${UNSET_VAR}"` and `$UNSET_VAR` is not set
- **THEN** the parsed config struct SHALL have `Jira.APIToken` set to an empty string

### Requirement: Default Values

The config parser SHALL apply default values for optional fields when they are not specified in the config file. The following defaults SHALL apply:
- `jira.sync_interval_seconds`: 60
- `board.columns`: the five default columns (Backlog, Ready, Doing, Review, Done) with their standard Jira status mappings as defined in spec.md section 4.2
- `theme`: "default"
- `keybindings.vim_mode`: true

#### Scenario: Missing optional fields receive defaults
- **WHEN** a config file specifies only the `jira` section with `base_url`, `email`, and `api_token`
- **THEN** the parsed config SHALL have `jira.sync_interval_seconds` set to 60, five default board columns, `theme` set to "default", and `keybindings.vim_mode` set to true

#### Scenario: Explicitly set values override defaults
- **WHEN** a config file sets `jira.sync_interval_seconds` to 120
- **THEN** the parsed config SHALL have `jira.sync_interval_seconds` set to 120, not the default

### Requirement: Config File Not Found

The config parser SHALL NOT return an error when the config file does not exist. Instead, it SHALL return a config struct populated entirely with default values. This allows the application to start without a config file for initial setup or testing.

#### Scenario: No config file exists
- **WHEN** no config file exists at any resolved path
- **THEN** the parser SHALL return a config struct with all default values and no error

### Requirement: Config Struct Definition

The config struct SHALL include the following top-level sections:
- `Jira`: base URL, email, API token, project keys (string slice), JQL filter, sync interval seconds
- `Board`: columns (slice of column configs, each with name, jira_statuses slice, jira_transition_id)
- `Theme`: theme name string
- `Keybindings`: vim_mode boolean

All struct fields SHALL have `yaml` tags for proper YAML unmarshalling.

#### Scenario: Full config round-trips through parse
- **WHEN** a config struct is marshalled to YAML and then parsed back
- **THEN** the resulting struct SHALL be equal to the original

### Requirement: Database Path Resolution

The config parser SHALL provide a function to resolve the database file path using the following precedence:
1. If `db.path` is set in the config, use that value.
2. If `$XDG_DATA_HOME` is set, use `$XDG_DATA_HOME/legato/legato.db`.
3. Otherwise, use `~/.local/share/legato/legato.db`.

#### Scenario: DB path from config
- **WHEN** the config file sets `db.path` to `/tmp/legato-test.db`
- **THEN** the resolved database path SHALL be `/tmp/legato-test.db`

#### Scenario: DB path from XDG_DATA_HOME
- **WHEN** `db.path` is not set and `$XDG_DATA_HOME` is `/home/user/.data`
- **THEN** the resolved database path SHALL be `/home/user/.data/legato/legato.db`

#### Scenario: DB path uses default
- **WHEN** `db.path` is not set and `$XDG_DATA_HOME` is not set
- **THEN** the resolved database path SHALL be `~/.local/share/legato/legato.db`
