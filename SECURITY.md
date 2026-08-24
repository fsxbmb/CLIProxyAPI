# Security policy

This project is designed for one user's Mac and deliberately refuses non-loopback API hosts or remote management.

Do not expose ports 8317 or 8318 through router forwarding, tunnels, reverse proxies or public interfaces. Do not commit the generated data directory. Treat OAuth files, `secrets.json`, Management keys and local API keys as credentials.

For a suspected vulnerability, open a private GitHub security advisory in this repository rather than a public issue. Include the affected commit, reproduction steps and whether any credential may have been exposed. Revoke affected provider sessions and rotate local keys before sharing diagnostic material.
