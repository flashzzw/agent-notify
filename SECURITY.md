# Security and privacy

Agent Notify runs locally. The desktop channel invokes Windows PowerShell. The WeCom channel sends event descriptions, message text and available workspace information to the configured group bot. Agent-provided messages may include private project content.

The webhook URL contains the bot credential. Setup stores it in the local YAML configuration; backups can contain previous credentials. Treat the entire configuration directory as private. `config show` redacts webhook fields, but that does not make raw files or all log content safe to publish. Review and redact any diagnostic material before sharing it.

Only grant access to notification channels appropriate for the information your agents process. If a webhook is exposed, rotate or remove the bot credential and update the local configuration and backups.

## Reporting a vulnerability

Use GitHub's private **Report a vulnerability** option on this repository's Security page when available. Do not post working credentials, private configuration, or an exploitable vulnerability in a public issue. If private reporting is unavailable, open a minimal issue asking the maintainer for a private contact route without including sensitive details.

Security fixes target the latest release of this edition; older releases have no separate maintenance promise.
