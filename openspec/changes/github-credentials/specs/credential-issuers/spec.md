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

### Requirement: oauth2 issuer
The `oauth2` issuer SHALL support the grant types `client_credentials`, `refresh_token`, and RFC 8693 token exchange, with client authentication `client_secret_basic` or `client_secret_post`. When the provider returns a new refresh token, the issuer SHALL write it back through the store's `Putter`. When the profile names a revocation endpoint, it SHALL revoke tokens per RFC 7009.

#### Scenario: Rotated refresh token
- **WHEN** a refresh response carries a new refresh token
- **THEN** the issuer SHALL store it, and the next refresh SHALL use it

#### Scenario: Store cannot write
- **WHEN** a credential that rotates refresh tokens names a store without `Putter`
- **THEN** loading the configuration SHALL fail with an error that names the credential

#### Scenario: Token exchange
- **WHEN** a profile uses token exchange with a subject credential
- **THEN** the request SHALL carry `grant_type=urn:ietf:params:oauth:grant-type:token-exchange` and the subject credential's token as `subject_token`

### Requirement: ssh-cert issuer
The `ssh-cert` issuer SHALL make a fresh Ed25519 key for a grant and an OpenSSH user certificate for it, signed with the credential's CA key. The certificate's key ID SHALL be the execution ID, its principals SHALL be the users the grant covers, it SHALL be valid from 30 seconds before issue to 5 minutes after, and it SHALL permit a terminal only (`permit-pty`), with no agent, port or X11 forwarding. The fresh private key SHALL stay in Legato's memory.

#### Scenario: Certificate contents
- **WHEN** a grant for `ssh://deploy@web1.lan` needs a certificate
- **THEN** the certificate SHALL have key ID equal to the execution ID, the principal `deploy`, and a validity of 5 minutes and 30 seconds

#### Scenario: Expired certificate
- **WHEN** the `ssh-agent` channel needs to sign after the certificate has expired
- **THEN** the issuer SHALL make a new key and certificate

#### Scenario: Login with the certificate
- **WHEN** an `sshd` trusts the CA through `TrustedUserCAKeys`
- **THEN** an agent's `ssh deploy@web1.lan` through the execution's socket SHALL log in when the grant allows it

### Requirement: The ssh-ca profile
Legato SHALL ship a profile named `ssh-ca` with the `ssh-cert` issuer, the `ssh-agent` channel, the `ssh` inspector and the same default rules as the `ssh` profile.

#### Scenario: Profile loads
- **WHEN** a credential names profile `ssh-ca` and a `keypair` secret
- **THEN** loading the configuration SHALL succeed
