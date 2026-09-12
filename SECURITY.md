# Security Policy

## Reporting a vulnerability

Please do not open a public issue for security problems.

Use GitHub's private vulnerability reporting: open the **Security** tab on the repository and choose
**Report a vulnerability**. That creates a private advisory that only the maintainer can see.

Include what you can: the affected version (`aat --version`), steps to reproduce, and what an
attacker could do with it.

## Supported versions

Only the latest minor release line receives security fixes. If you are on an older release, upgrade
first and check whether the problem still reproduces.

## Response

AAT has a single maintainer, so response is best effort and there is no SLA. Reports are read and
acknowledged as they come in, and a fix or workaround follows as soon as practical. Reporters are
credited in the advisory and release notes unless they ask otherwise.

## Things to know when running AAT

- **Run archives redact credentials, not everything.** `Authorization`, `Proxy-Authorization`,
  `X-API-Key`, `X-Auth-Token`, `Cookie`, and `Set-Cookie` values are replaced with `[REDACTED]`
  before an archive or `.aar` export is written, and the resolved value of every configured secret
  credential (environment, host override, plan, and overlay auth, except the oauth2 `username` and
  `clientId`, and the LLM API key) is redacted from every string in the archive, request and response
  bodies and URLs included; a secret shorter than eight characters only where a whole value equals it.
  Tokens an API issues at run time (outside credential headers) and personal data it returns are
  stored as-is, and terminal and `--json` output are not redacted, so review an archive before
  sharing it.
- **`--dump-state` files contain live credentials.** The live-state export written by
  `aat run plan --dump-state FILE` includes each step's base URL and live request headers, the
  default route's auth headers, and step outputs, so an external harness can pick up where a run
  left off. The file is written with mode `0600`, including when it replaces an existing file.
  Never commit these files, attach them to issues, or leave them in a shared location.
- **Lua transforms have limited reach, but they are not a sandbox.** A template's transform cannot
  use `io`, `os`, or `package`, and it cannot load or run code from files or strings: `dofile`,
  `loadfile`, `load`, `loadstring`, and `require` are removed. It still runs inside the `aat` process
  with no memory limit, and its 5-second timeout does not interrupt a long call into a library
  function. Review the transforms in templates you did not write, such as an integration kit's.
- **`aat mcp serve --http` has no authentication.** The HTTP transport listens on `127.0.0.1`
  unless `--host` (or the `AAT_HOST` environment variable) says otherwise, and accepts any request
  that reaches it. Bind another interface only on a machine that untrusted networks cannot reach,
  or put it behind a reverse proxy that handles authentication and TLS. The default stdio
  transport does not listen on the network at all.
- **`aat web` is a local tool.** The web UI listens on `127.0.0.1` by default and serves your run
  archives, including rename and import, to anyone who can reach the port. `--host 0.0.0.0`
  exposes it to your network; do not do that on untrusted networks. The Docker image sets
  `AAT_HOST=0.0.0.0` because a container's loopback is unreachable through a published port, so
  publish the port (`-p`) only where that exposure is acceptable.
