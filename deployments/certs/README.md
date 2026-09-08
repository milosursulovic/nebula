# Dev-only TLS cert

`nebula-agent.crt` / `nebula-agent.key` is a self-signed cert (SANs:
`nebula-agent`, `localhost`, `127.0.0.1`) checked into the repo for local
compose use only — same "dev-only, change-me" precedent as the plaintext
`JWT_SECRET`/`NEBULA_NODE_BOOTSTRAP_SECRET` values already in
`docker-compose.yml`. It secures the control-plane → agent gRPC channel
(Phase 10: TLS now, mTLS later). Never reuse this key pair outside local
development.

Regenerate with:

```
openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -keyout nebula-agent.key -out nebula-agent.crt \
  -subj "/CN=nebula-agent" \
  -addext "subjectAltName=DNS:nebula-agent,DNS:localhost,IP:127.0.0.1"
```
