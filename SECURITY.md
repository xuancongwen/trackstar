# Security policy

Trackstar is a self-hosted application that stores account credentials and
session data, so security reports are taken seriously.

## Reporting a vulnerability

Please do not open a public issue for a security problem. Use GitHub's private
vulnerability reporting instead:

<https://github.com/xuancongwen/trackstar/security/advisories/new>

Include the version (`trackstar version` or the release tag), steps to
reproduce, and the impact you believe it has. You should hear back within a
week. Once a fix is released the advisory is published with credit to the
reporter unless you prefer otherwise.

## Supported versions

Only the latest release receives fixes. Self-hosters should follow the
[Updates and deploys](README.md#updates-and-deploys) section of the README.

## OAuth for MCP clients

What to expect from the OAuth flow, so a report can say where it departs
from it:

- Registration (`POST /oauth/register`) is open to anyone and rate limited
  per address. A registration grants nothing: a client can act only after a
  signed-in user approves it on the consent screen, and its name there is
  self-declared.
- Clients are public. PKCE with S256 is required; redirect URIs must be
  https or loopback http and are matched against the registered ones.
- A connected app acts as its user on project data through `/mcp` only. Its
  token is not accepted by `/api`, so it cannot change passwords, manage
  accounts, mint API tokens or approve other apps. There are no scopes.
- Authorization codes, access tokens and refresh tokens are stored as HMACs
  keyed with the session secret; a copy of the database does not contain
  usable credentials.
- Codes are single use and expire after a minute. Refresh tokens rotate on
  every use. Presenting a code or a refresh token a second time revokes the
  whole connection.
- Disconnecting an app, changing or resetting the password, and deactivating
  the account each revoke the tokens immediately.
