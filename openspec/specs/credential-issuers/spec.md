## ADDED Requirements

### Requirement: Templated jwt-exchange issuer
The `jwt-exchange` issuer SHALL sign a JWT whose claims come from the profile's template, with the credential's `keypair` secret, and SHALL send it to the profile's exchange endpoint, optionally after a lookup request. It SHALL read the token and its expiry from the response by the JSON paths the profile names. Templates SHALL only substitute values from the grant, the credential's settings and the clock, and SHALL NOT name a host that the profile does not name.

#### Scenario: GitHub installation token for one repository
- **WHEN** a grant for `git.read` on `github:acme/widget` needs a token
- **THEN** the issuer SHALL post to `/app/installations/<id>/access_tokens` with `repositories: ["widget"]` and `permissions: {contents: read, metadata: read}`
- **AND** SHALL return the response's `token` with the expiry from `expires_at`

#### Scenario: Key in a signing store
- **WHEN** the credential's store implements `Signer`
- **THEN** the issuer SHALL sign the JWT through the store and SHALL NOT read the private key

#### Scenario: Token reused
- **WHEN** a cached token for the same repository and permissions has more than 5 minutes left
- **THEN** the issuer SHALL return it without a request

#### Scenario: Template names an unknown value
- **WHEN** a profile's template names a placeholder that is not defined
- **THEN** loading the profile SHALL fail with an error that names the placeholder

### Requirement: Revoking issued tokens
When a profile names a revocation request, the issuer SHALL revoke a token when its grant ends or when the channel that asked for it is done with it. For GitHub this SHALL be `DELETE /installation/token` authenticated with the token itself.

#### Scenario: Grant ends
- **WHEN** the execution that holds a grant becomes terminal
- **THEN** the issuer SHALL revoke every token issued for that grant
