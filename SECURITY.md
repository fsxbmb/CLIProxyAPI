# Security policy

CLIProxyAPI Lite is designed for one user's local machine on macOS, Linux, or Windows. The API and Web UI bind to loopback by default and remote management is disabled.

Do not expose ports 8317 or 8318 through router forwarding, tunnels, reverse proxies, public interfaces, or a network-wide bind address. For a remote Linux host, use an authenticated SSH tunnel instead of changing `host` to `0.0.0.0`.

Do not commit the generated data directory. Treat the following as credentials:

- OAuth files under `auth/`;
- `secrets.json`;
- Management keys;
- local API keys;
- OpenAI-compatible upstream API keys in `config.yaml`.

The application uses the OS user configuration directory by default and applies private permissions on Unix-like systems. On Windows it uses the current user's `%AppData%` directory and hardens generated paths with the current user's ACL when `icacls` is available.

For a suspected vulnerability, open a private GitHub security advisory rather than a public issue. Include the platform, version, reproduction steps, and whether any credential may have been exposed. Revoke affected provider sessions and rotate local keys before sharing diagnostic material.
