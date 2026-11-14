# Security Policy

## Reporting a vulnerability

Please report security issues privately — do **not** open a public issue.

Use GitHub's private vulnerability reporting on this repository
(**Security → Report a vulnerability**). We aim to acknowledge reports within
a few days and will coordinate a fix and disclosure timeline with you.

## Supported versions

Security fixes land on the latest `main`; there are no maintained
release branches yet.

## Scope

The Local CLI and optional review UI assume a trusted machine. The listener
binds only to loopback and trusts local processes and accounts that can reach
it. Its browser session, exact origin, and CSRF checks protect interactions
from other browser origins. Local user names are cooperative attribution.

Report unintended request access, data exposure, unsafe local file or process
handling, release and installer integrity failures, and violations of those
boundaries. See [Trust and security](docs/using-specgate/concepts/trust-and-security.md).
