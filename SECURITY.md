# Security

## Report a vulnerability

Report security problems privately through [GitHub's private vulnerability reporting](https://github.com/trucke/garner/security/advisories/new), not in a public issue. Describe the problem, the version you use and, if you can, the steps to reproduce it.

You get an answer within a week. garner is maintained in spare time, so a fix can take longer; the advisory keeps you informed.

## Supported versions

Only the newest release gets security fixes.

## What garner protects

- Clients authenticate with a token, which `garnerd` stores only as a hash.
- Clients that connect to garner directly beyond loopback get HTTPS only. With garner's own certificate, the connection code carries the certificate's key pin, and clients refuse a server with another key. Behind a reverse proxy (`proxied = true`), the proxy holds a publicly trusted certificate and garner serves plain HTTP, which only the proxy may reach; firewall the listener.
- Admin commands such as `garnerd token create` reach the server through a socket in the data folder that only garner's user may open, not over the network.
- The image runs as UID 1000 and works on a read-only root filesystem, as the install guides run it. Its FFprobe reads only the video formats garner checks, with no network access and no decoders. It runs without garner's environment, so it never sees the API keys.
