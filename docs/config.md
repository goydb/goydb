# Configuration Reference

goydb is configured through three mechanisms, applied in this order:

1. **Environment variables** — read at startup, cannot be changed at runtime
2. **CLI flags** — override the corresponding environment variable
3. **Config store (`_config.json`)** — persisted JSON file in the database directory, editable at runtime via the `/_config` HTTP API

The config store is seeded with defaults on first run. Every write through the API is atomically persisted (temp file + rename) so values survive restarts.

---

## Environment Variables & CLI Flags

These settings are only read at startup. They control paths, the listen address, the cookie signing secret, and the initial admin accounts.

| Env Variable | CLI Flag | Default | Description |
|---|---|---|---|
| `GOYDB_DB_DIR` | `-dbs` | `./dbs` | Directory where databases and attachments are stored |
| `GOYDB_PUBLIC` | `-public` | `./public` | Directory for static files served by the built-in file server |
| `GOYDB_ENABLE_PUBLIC` | — | `true` | Enable/disable serving the public directory |
| `GOYDB_LISTEN` | `-addr` | `:7070` | Address and port to listen on (overridden by `httpd/bind_address` + `httpd/port` from the config store) |
| `GOYDB_SECRET` | `-cookie-secret` | *(built-in default)* | Hex-encoded cookie signing secret (generate with `openssl rand -hex 32`) |
| `GOYDB_ADMINS` | `-admins` | `admin:secret` | Comma-separated `user:password` pairs for server admin accounts |

Example:

```bash
GOYDB_DB_DIR=/var/lib/goydb \
GOYDB_LISTEN=0.0.0.0:5984 \
GOYDB_ADMINS=alice:s3cret,bob:hunter2 \
  goydb
```

Or with flags:

```bash
goydb -dbs /var/lib/goydb -addr 0.0.0.0:5984 -admins alice:s3cret,bob:hunter2
```

---

## Section `couchdb`

Instance identity, size limits, and validation behaviour.

| Key | Default | Description |
|---|---|---|
| `uuid` | `0dbc95c8-4208-11eb-ad76-00155d4c9c92` | Unique instance identifier returned by `GET /` |
| `version` | `0.1.0` | Reported CouchDB-compatible version string |
| `max_document_size` | `8000000` | Maximum document body size in bytes. `0` = unlimited |
| `max_dbs` | `0` | Maximum number of databases. `0` = unlimited |
| `max_docs_per_db` | `0` | Maximum number of documents per database. `0` = unlimited |
| `max_attachment_size` | `0` | Maximum single attachment size in bytes. `0` = unlimited |
| `max_db_size` | `0` | Maximum database file size in bytes. `0` = unlimited |
| `validate_on_replication` | `false` | When `true`, run all `validate_doc_update` functions on replication writes (`new_edits=false`). When `false` (default), VDU functions only run on replication writes if the individual design document has `"validate_on_replication": true` |

When a limit is exceeded the server returns:
- **413** for `max_document_size` and `max_attachment_size`
- **412** for `max_dbs`, `max_docs_per_db`, and `max_db_size`

Example — set a 4 MB document size limit:

```bash
curl -X PUT http://admin:secret@localhost:7070/_config/couchdb/max_document_size \
  -d '"4000000"'
```

---

## Section `chttpd`

HTTP server tuning.

| Key | Default | Description |
|---|---|---|
| `max_http_request_size` | `4294967296` (4 GB) | Maximum HTTP request body size in bytes |
| `admin_only_all_dbs` | `true` | When `true`, `GET /_all_dbs` and `POST /_dbs_info` require server admin credentials. Set to `false` to allow any authenticated user |

Example — allow any user to list databases:

```bash
curl -X PUT http://admin:secret@localhost:7070/_config/chttpd/admin_only_all_dbs \
  -d '"false"'
```

---

## Section `httpd`

HTTP listener settings and the authentication handler chain.

| Key | Default | Description |
|---|---|---|
| `bind_address` | `0.0.0.0` | IP address to bind to. At startup, this value (combined with `port`) overrides the `GOYDB_LISTEN` env var |
| `port` | `7070` | TCP port number. Combined with `bind_address` at startup |
| `enable_cors` | `false` | Enable CORS middleware (reads settings from the `cors` section) |
| `authentication_handlers` | `cookie_authentication_handler,default_authentication_handler` | Comma-separated ordered list of authentication handler names |

Available authentication handler names:

| Name | Method | Description |
|---|---|---|
| `cookie_authentication_handler` | Cookie/Session | Restores sessions from the `AuthSession` cookie |
| `default_authentication_handler` | HTTP Basic Auth | Validates `Authorization: Basic` credentials |
| `proxy_authentication_handler` | Proxy Auth | Trusts reverse-proxy headers |
| `jwt_authentication_handler` | JWT Bearer | Validates `Authorization: Bearer` tokens |

Example — enable all four handlers:

```bash
curl -X PUT http://admin:secret@localhost:7070/_config/httpd/authentication_handlers \
  -d '"cookie_authentication_handler,default_authentication_handler,proxy_authentication_handler,jwt_authentication_handler"'
```

---

## Section `couch_httpd_auth`

Proxy authentication settings. Only relevant when `proxy_authentication_handler` is in the handler chain.

| Key | Default | Description |
|---|---|---|
| `proxy_use_secret` | `false` | When `true`, require HMAC-SHA1 token verification on proxy requests |
| `secret` | `""` | Shared secret for HMAC-SHA1 token computation |
| `x_auth_username` | `X-Auth-CouchDB-UserName` | HTTP header carrying the username |
| `x_auth_roles` | `X-Auth-CouchDB-Roles` | HTTP header carrying comma-separated roles |
| `x_auth_token` | `X-Auth-CouchDB-Token` | HTTP header carrying the HMAC-SHA1 token |

The HMAC token is computed as `hex(HMAC-SHA1(username, secret))`.

**Security note:** Without `proxy_use_secret`, anyone who can reach goydb directly (bypassing the proxy) can impersonate any user by setting the headers. Either enable the secret or ensure goydb is only reachable through the proxy.

Example — enable proxy auth with secret verification:

```bash
curl -X PUT http://admin:secret@localhost:7070/_config/httpd/authentication_handlers \
  -d '"cookie_authentication_handler,default_authentication_handler,proxy_authentication_handler"'

curl -X PUT http://admin:secret@localhost:7070/_config/couch_httpd_auth/proxy_use_secret \
  -d '"true"'

curl -X PUT http://admin:secret@localhost:7070/_config/couch_httpd_auth/secret \
  -d '"my-shared-secret"'
```

---

## Section `jwt_keys`

Static JWT key material. Keys are stored as `{algorithm}` or `{algorithm}:{kid}` entries.

| Key Format | Value | Description |
|---|---|---|
| `HS256` | `my-secret` | HMAC shared secret (plain string) |
| `RS256` | `-----BEGIN PUBLIC KEY-----...` | PEM-encoded RSA public key |
| `ES256` | `-----BEGIN PUBLIC KEY-----...` | PEM-encoded EC public key |
| `RS256:my-kid` | PEM key | RSA key matched by `kid` header in the JWT |

Supported algorithms: HS256, HS384, HS512, RS256, RS384, RS512, ES256, ES384, ES512.

Key resolution order:
1. Static keys — looks up `{alg}:{kid}` then `{alg}` in this section
2. JWKS URL — if no static key matches and `jwks_url` is configured, fetches the remote endpoint and matches by `kid`

Example — add an HMAC key:

```bash
curl -X PUT http://admin:secret@localhost:7070/_config/jwt_keys/HS256 \
  -d '"my-shared-secret"'
```

---

## Section `jwt_auth`

JWT authentication behaviour. Only relevant when `jwt_authentication_handler` is in the handler chain.

| Key | Default | Description |
|---|---|---|
| `required_claims` | `""` | Comma-separated `key=value` pairs that must be present in the token. Example: `iss=myapp,aud=goydb` |
| `roles_claim_path` | `_couchdb.roles` | Dot-separated path to the roles array in the JWT claims. Example: `realm_access.roles` for Keycloak |
| `jwks_url` | `""` | URL of a remote JWKS endpoint for key discovery (e.g. `https://idp.example.com/.well-known/jwks.json`) |
| `jwks_cache_ttl` | `3600` | Seconds to cache JWKS keys before re-fetching |

Token requirements:
- Must contain a `sub` claim (mapped to the goydb username)
- Must contain an `exp` claim (standard JWT expiry)
- Roles are extracted from the path specified by `roles_claim_path`

Example — configure for Keycloak:

```bash
curl -X PUT http://admin:secret@localhost:7070/_config/jwt_auth/jwks_url \
  -d '"https://keycloak.example.com/realms/myrealm/protocol/openid-connect/certs"'

curl -X PUT http://admin:secret@localhost:7070/_config/jwt_auth/required_claims \
  -d '"iss=https://keycloak.example.com/realms/myrealm"'

curl -X PUT http://admin:secret@localhost:7070/_config/jwt_auth/roles_claim_path \
  -d '"realm_access.roles"'
```

---

## Section `cors`

CORS policy. Only applied when `httpd/enable_cors` is `true`.

| Key | Default | Description |
|---|---|---|
| `origins` | `*` | Comma-separated list of allowed origins |
| `credentials` | `false` | Allow credentials (`Access-Control-Allow-Credentials`) |
| `headers` | `accept, authorization, content-type, origin, referer` | Comma-separated list of allowed request headers |
| `methods` | `GET, PUT, POST, HEAD, DELETE` | Comma-separated list of allowed HTTP methods |

Example — enable CORS for a specific origin:

```bash
curl -X PUT http://admin:secret@localhost:7070/_config/httpd/enable_cors \
  -d '"true"'

curl -X PUT http://admin:secret@localhost:7070/_config/cors/origins \
  -d '"https://myapp.example.com"'

curl -X PUT http://admin:secret@localhost:7070/_config/cors/credentials \
  -d '"true"'
```

---

## Section `log`

Logging configuration. Read at startup to create the logger.

| Key | Default | Description |
|---|---|---|
| `level` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `file` | `""` | Path to a log file. Empty = stdout |

Example — enable debug logging to a file:

```bash
curl -X PUT http://admin:secret@localhost:7070/_config/log/level \
  -d '"debug"'

curl -X PUT http://admin:secret@localhost:7070/_config/log/file \
  -d '"/var/log/goydb.log"'
```

---

## Section `admins`

Runtime admin accounts stored in the config store. These are in addition to the admins configured via `GOYDB_ADMINS` / `-admins` at startup.

Admin accounts added through the config API are persisted in `_config.json`:

```bash
# Add an admin
curl -X PUT http://admin:secret@localhost:7070/_config/admins/alice \
  -d '"s3cret"'

# Remove an admin
curl -X DELETE http://admin:secret@localhost:7070/_config/admins/alice
```

---

## TOTP Two-Factor Authentication

TOTP is not a server config section — it is configured per-user in `_users` documents. When a user document contains a `totp.key` field, a valid TOTP token is required during `POST /_session` login.

User document setup:

```json
{
  "_id": "org.couchdb.user:alice",
  "name": "alice",
  "type": "user",
  "roles": [],
  "password": "secret",
  "totp": {
    "key": "JBSWY3DPEHPK3PXP"
  }
}
```

The `key` is a Base32-encoded shared secret. TOTP uses RFC 6238 (HMAC-SHA1, 30-second steps, 6 digits) with a +/-1 step window for clock drift.

Login with TOTP:

```bash
curl -X POST http://localhost:7070/_session \
  -H "Content-Type: application/json" \
  -d '{"name": "alice", "password": "secret", "token": "123456"}'
```

Notes:
- Server admin accounts (from `GOYDB_ADMINS` or the `admins` config section) bypass TOTP
- TOTP only applies to `POST /_session` — Basic Auth, Proxy, and JWT handlers are not affected
- There is no global toggle — TOTP activates per-user when `totp.key` is present

---

## Config API

All config endpoints require server admin authentication. Both the flat path (`/_config`) and the CouchDB 2.x node-scoped path (`/_node/{node}/_config`) are supported — they access the same store.

### Get all configuration

```bash
curl http://admin:secret@localhost:7070/_config
```

### Get a section

```bash
curl http://admin:secret@localhost:7070/_config/couchdb
```

### Get a single key

```bash
curl http://admin:secret@localhost:7070/_config/couchdb/max_document_size
```

Returns the value as a JSON string: `"8000000"`

### Set a key

```bash
curl -X PUT http://admin:secret@localhost:7070/_config/couchdb/max_document_size \
  -d '"4000000"'
```

The request body must be a JSON-encoded string. Returns the previous value.

### Delete a key

```bash
curl -X DELETE http://admin:secret@localhost:7070/_config/couchdb/max_document_size
```

Returns the deleted value. Returns 404 if the key does not exist.

### Reload (no-op)

```bash
curl -X POST http://admin:secret@localhost:7070/_node/_local/_config/_reload
```

Since goydb is a single-node embedded server, config is always in-memory and this endpoint is a no-op that returns `{"ok": true}`.
