# Security Policy

## Reporting a vulnerability

Please report security issues privately — do **not** open a public issue.

Use GitHub's private vulnerability reporting on this repository
(**Security → Report a vulnerability**). We aim to acknowledge reports within
a few days and will coordinate a fix and disclosure timeline with you.

## Supported versions

SpecGate is pre-1.0. Security fixes land on the latest `main`; there are no maintained
release branches yet.

## Scope

SpecGate's Doc Registry is designed as an **internal/network-trusted** service (no HTTP
auth by design). Reports about the absence of HTTP authentication on Doc Registry are
out of scope; reports about unintended exposure, credential leakage, injection, or
privilege issues are in scope.

The alpha release also includes a web UI. Treat the UI, Doc Registry,
agents service, and MCP endpoint as local or trusted-network surfaces unless you
place an authentication proxy and TLS boundary in front of them.
