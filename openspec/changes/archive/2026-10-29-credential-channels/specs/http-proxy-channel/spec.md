## ADDED Requirements

### Requirement: Loopback proxy with a fixed upstream
The host Legato SHALL serve the `http-proxy` channel on a loopback listener, with the prefix `/c/<credential>/` for each credential that uses it. It SHALL forward a request only to the upstream that the credential's profile or settings fix, over TLS with certificate checks. The agent SHALL NOT be able to choose another upstream.

#### Scenario: Request forwarded to the upstream
- **WHEN** an agent sends `POST http://127.0.0.1:<port>/c/elevenlabs/v1/text-to-speech/abc` with its placeholder
- **THEN** Legato SHALL send `POST https://api.elevenlabs.io/v1/text-to-speech/abc` with the real key
- **AND** SHALL stream the upstream's response back to the agent

#### Scenario: Absolute URL in the request
- **WHEN** a request line names a host other than the listener, as a forward proxy request would
- **THEN** Legato SHALL refuse it with `400` and SHALL contact no host

#### Scenario: Redirect from the upstream
- **WHEN** the upstream answers `302` with a `Location` on another host
- **THEN** Legato SHALL pass the response back unchanged and SHALL NOT follow it

### Requirement: Placeholder keys tied to an execution
When the channel opens for an execution, Legato SHALL create a random placeholder, store only its SHA-256 hash with the execution and the credential, and give the agent the placeholder and the base URL in the variables the profile names. The proxy SHALL accept a placeholder only for its own credential and only while its execution is running. The real key SHALL never be sent to the agent.

#### Scenario: No placeholder
- **WHEN** a request to `/c/elevenlabs/` carries no placeholder in the key's place
- **THEN** Legato SHALL answer `401` and SHALL NOT forward it

#### Scenario: Placeholder of another credential
- **WHEN** a placeholder made for `openai` is sent to `/c/elevenlabs/`
- **THEN** Legato SHALL answer `401`

#### Scenario: Execution ended
- **WHEN** a request carries the placeholder of an execution that is terminal
- **THEN** Legato SHALL answer `401`

#### Scenario: Key replaced
- **WHEN** a request is forwarded
- **THEN** the upstream SHALL receive the real key in the profile's place
- **AND** SHALL NOT receive the placeholder in any header, query parameter or body

#### Scenario: Placeholder not stored in plain form
- **WHEN** the database is inspected after a channel opens
- **THEN** it SHALL contain the placeholder's hash and SHALL NOT contain the placeholder

### Requirement: Each request authorized before it is forwarded
The proxy SHALL classify each request with the profile's inspector and SHALL call `CredentialBroker.Authorize` with the operation, the resource and the amounts known before forwarding. It SHALL forward the request only when the use is within a grant. A use that needs the person SHALL be held up to 60 seconds; when no decision arrives, the proxy SHALL answer `403` with a JSON body that names the approval request ID.

#### Scenario: Use within a grant
- **WHEN** a request is classified as an operation that a grant covers
- **THEN** Legato SHALL forward it and record the use in the audit log

#### Scenario: Use denied
- **WHEN** the matching rule is `deny`
- **THEN** Legato SHALL answer `403` with a body that names the operation and resource
- **AND** SHALL NOT forward the request

#### Scenario: No decision within 60 seconds
- **WHEN** a request needs the person and 60 seconds pass with no decision
- **THEN** Legato SHALL answer `403` with `{"approval": "ap-…", "status": "pending"}`
- **AND** the approval request SHALL stay open

### Requirement: The rest inspector
The `rest` inspector SHALL classify a request by the first route of the profile whose method and path pattern match, and SHALL produce the route's operation and a resource of the upstream host and the route's resource template, or the request path when the route has none. It SHALL refuse a request that matches no route.

#### Scenario: Matching route
- **WHEN** a request is `POST /v1/text-to-speech/abc` and a route matches `POST /v1/text-to-speech/{voice}` with resource `voice/{voice}`
- **THEN** the operation SHALL be the route's operation and the resource SHALL be `api.elevenlabs.io/voice/abc`

#### Scenario: No matching route
- **WHEN** a request matches no route
- **THEN** Legato SHALL answer `403` with a body that names the method and path, and SHALL NOT forward it

### Requirement: Meters
The proxy SHALL count one `request` for each proxied request, `bytes` as the request body length plus the response body length, and `characters` as the Unicode code points at the JSON path a route names. It SHALL check requests and characters against a rule's limit before forwarding. It SHALL add response bytes to the totals when the response ends. Totals SHALL be kept in the store for each credential, meter and period.

#### Scenario: Characters counted
- **WHEN** a metered request's body has `"text": "héllo"`
- **THEN** the use SHALL carry 5 characters

#### Scenario: Limit reached
- **WHEN** a request would bring the day's characters above a rule's limit
- **THEN** the use SHALL be decided by the next matching rule

#### Scenario: Restart keeps totals
- **WHEN** Legato restarts during a day
- **THEN** the day's totals SHALL be those recorded before the restart

#### Scenario: Metered body too large
- **WHEN** a request on a route with a characters meter has a body larger than 1 MiB
- **THEN** Legato SHALL answer `413` and SHALL NOT forward it

### Requirement: The elevenlabs profile
Legato SHALL ship a profile named `elevenlabs` with upstream `https://api.elevenlabs.io`, the key in header `xi-api-key`, a characters meter on text to speech, and these default rules: text to speech `auto` up to 20,000 characters a day and `window` of 1 hour after that; voice and model reads `auto`; account, workspace and key management `deny`.

#### Scenario: Under the daily limit
- **WHEN** an agent requests speech for 500 characters and the day's total is 1,000
- **THEN** the request SHALL be forwarded without an approval request

#### Scenario: Key management
- **WHEN** an agent sends a request to an API key management route
- **THEN** Legato SHALL refuse it

### Requirement: The api-key profile
Legato SHALL ship a profile named `api-key` whose credential settings name an `https` upstream and the key's place: a header with a format that contains `{secret}`, or a query parameter. It SHALL classify `GET` and `HEAD` as `http.read` and every other method as `http.write`, with the request path as the resource. Its default rules SHALL be `session` for `http.read` and `request` for `http.write`.

#### Scenario: Invalid settings
- **WHEN** an `api-key` credential names an `http` upstream or no key place
- **THEN** loading the configuration SHALL fail with an error that names the credential

#### Scenario: Bearer format
- **WHEN** `settings.key` is `{header: Authorization, format: "Bearer {secret}"}`
- **THEN** the upstream SHALL receive `Authorization: Bearer <real key>`
