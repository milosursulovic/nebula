# Dev-only TLS certs

Three self-signed cert pairs checked into the repo for local compose use
only — same "dev-only, change-me" precedent as the plaintext
`JWT_SECRET`/`NEBULA_NODE_BOOTSTRAP_SECRET` values already in
`docker-compose.yml`. Never reuse any of these outside local development.

- **`nebula-agent.crt`/`.key`** (SANs: `nebula-agent`, `localhost`,
  `127.0.0.1`) — `nebula-agent`'s gRPC server identity. Phase 10 pinned
  this as the trust anchor for server-authenticated TLS; Phase 18 adds
  mutual auth on top (below), this cert's role is unchanged.
- **`nebula-api-client.crt`/`.key`** (CN `nebula-api-client`) — Phase
  18's mTLS: `nebula-api`'s *client* identity when it dials
  `nebula-agent`. `nebula-agent` requires and verifies this
  (`tls.RequireAndVerifyClientCert`, `ClientCAs` pinned to this exact
  cert — same "self-signed cert is its own trust anchor" pattern as the
  server side) — a client without it is rejected outright, closing the
  Phase 10/section 51 deferral ("Use mTLS later").
- **`nebula-api.crt`/`.key`** (SANs: `nebula-api`, `localhost`,
  `127.0.0.1`) — Phase 18's *optional* TLS for `nebula-api`'s own HTTP
  surface. Config-gated (`NEBULA_TLS_CERT_FILE`/`NEBULA_TLS_KEY_FILE`),
  **off by default** in `docker-compose.yml` — every existing plain
  `curl http://localhost:8080/...` example keeps working unchanged;
  this cert exists so the TLS path is real and verifiable, not
  unexercised code.

Regenerate any of them with the same pattern (swap name/CN/SANs):

```
openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -keyout nebula-agent.key -out nebula-agent.crt \
  -subj "/CN=nebula-agent" \
  -addext "subjectAltName=DNS:nebula-agent,DNS:localhost,IP:127.0.0.1"

openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -keyout nebula-api-client.key -out nebula-api-client.crt \
  -subj "/CN=nebula-api-client" \
  -addext "subjectAltName=DNS:nebula-api-client,DNS:localhost,IP:127.0.0.1"

openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -keyout nebula-api.key -out nebula-api.crt \
  -subj "/CN=nebula-api" \
  -addext "subjectAltName=DNS:nebula-api,DNS:localhost,IP:127.0.0.1"
```
