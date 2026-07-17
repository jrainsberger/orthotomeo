# Security Policy

## Reporting a vulnerability

If you find a security issue in orthotomeo, please report it privately rather
than opening a public issue:

- Email: justin.rainsberger@gmail.com
- Or use GitHub's private vulnerability reporting, if enabled on this repo:
  `Security` tab -> `Report a vulnerability`

Please include a description of the issue and its potential impact, steps to
reproduce, and any relevant logs or proof-of-concept. I'll aim to acknowledge
within a few days and keep you updated on remediation.

## Scope

orthotomeo is a personal, unaffiliated project (see [README](README.md))
serving public-domain and CC-licensed biblical text through a read-only,
unauthenticated API - there's no user data, no auth, and no write surface, so
the practical impact of most vulnerability classes here is limited. I still
want to know about anything real: an injection vector, a way to bypass rate
limiting at scale, a way to access non-shippable/license-restricted source
data, or anything else that breaks the guarantees the project makes about
itself.

## Supported versions

This is a single, continuously-deployed project - only the latest version
(the `main` branch / latest deployed revision) is supported. There are no
older maintained release branches.
