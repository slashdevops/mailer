# Security Policy

## Supported Versions

While the project is pre-1.0, security fixes are applied to the latest released
minor version.

| Version | Supported          |
| ------- | ------------------ |
| 0.0.x   | :white_check_mark: |

## Reporting a Vulnerability

Please **do not** open a public issue for security vulnerabilities.

Report privately using GitHub's
[private vulnerability reporting](https://github.com/slashdevops/mailer/security/advisories/new)
for this repository. Include:

- a description of the issue and its impact,
- steps to reproduce or a proof of concept,
- affected version(s), and
- any suggested remediation.

You can expect an initial acknowledgement within a few business days. We will
work with you on a fix and coordinate disclosure once a patch is available.

## Security considerations when using this library

- Enable `RequireTLS` on the SMTP transport so messages and credentials are
  never sent over an unencrypted connection.
- Load SMTP credentials from a secret store or environment variables, never from
  source control.
- Build messages exclusively through `MailContentBuilder`; it rejects CR/LF/NUL
  in header fields to prevent SMTP header/command injection.
