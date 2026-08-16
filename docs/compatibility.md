# goydb CouchDB API Compatibility Matrix

Based on the [CouchDB 3.x API reference](https://docs.couchdb.org/en/stable/api/index.html).

Legend: **Yes** = fully implemented · **Partially** = implemented with gaps (see details) · **No** = not implemented

---

## Server API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| GET | `/` | **Yes** | Returns welcome message, version, features |
| GET | `/_active_tasks` | **Yes** | Returns task list with CouchDB-compatible types: indexer, search_indexer; compaction is a no-op (embedded), replication tracked via scheduler |
| GET | `/_all_dbs` | **Yes** | Lists databases; supports `startkey`, `endkey`, `limit`, `skip`, `descending` query params |
| POST | `/_dbs_info` | **Yes** | Returns info for multiple databases; handles missing DBs with error entries |
| GET | `/_db_updates` | **Yes** | Returns database events; normal feed lists all DBs as `updated` |
| GET | `/_membership` | **Yes** | Returns single-node membership |
| POST | `/_replicate` | **Yes** | Supports all params: `source`, `target`, `continuous`, `create_target`, `filter`, `query_params`, `doc_ids`, `selector`, `since_seq`, `use_checkpoints`, `checkpoint_interval`, `cancel` |
| GET | `/_scheduler/jobs` | **Yes** | Returns job list with history, info, error_count, last_updated |
| GET | `/_scheduler/docs` | **Yes** | Returns replication docs from _replicator with state details |
| GET | `/_scheduler/docs/{replication-id}` | **Yes** | Individual replication doc lookup |
| POST | `/_search_analyze` | **Yes** | Basic whitespace tokenization |
| POST | `/_nouveau_analyze` | **Yes** | Basic whitespace tokenization |
| GET | `/_up` | **Yes** | Returns `{"status":"ok"}` |
| GET | `/_uuids` | **Yes** | Supports `count` query param |
| GET | `/_utils` | **No** | Served externally via container setup |
| GET | `/favicon.ico` | **No** | User-provided |

### Cluster Setup

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| GET | `/_cluster_setup` | **Yes** | Returns `single_node_enabled` state |
| POST | `/_cluster_setup` | **Yes** | Accepts all actions: `enable_single_node`, `finish_cluster`, `enable_cluster`, `add_node`, `remove_node`; validates action field |
| GET | `/_reshard` | **Yes** | Returns resharding status (single-node: always empty) |
| GET | `/_reshard/state` | **Yes** | Returns resharding state |
| PUT | `/_reshard/state` | **Yes** | Accepts state update; no-op |
| GET | `/_reshard/jobs` | **Yes** | Returns empty jobs list |
| GET | `/_reshard/jobs/{jobid}` | **No** | |
| PUT | `/_reshard/jobs/{jobid}/state` | **No** | |

### Node API (`/_node/{node-name}/...`)

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| GET | `/_node/{node}` | **Yes** | Returns node name; `_local` maps to current node |
| GET | `/_node/{node}/_config` | **Yes** | Node name is ignored; uses same config store |
| GET | `/_node/{node}/_config/{section}` | **Yes** | |
| GET | `/_node/{node}/_config/{section}/{key}` | **Yes** | |
| PUT | `/_node/{node}/_config/{section}/{key}` | **Yes** | Returns old value |
| DELETE | `/_node/{node}/_config/{section}/{key}` | **Yes** | |
| POST | `/_node/{node}/_config/_reload` | **Yes** | Returns `{"ok": true}`; no-op for embedded server |
| GET | `/_node/{node}/_stats` | **Yes** | Returns basic request statistics structure |
| GET | `/_node/{node}/_prometheus` | **No** | Prometheus metrics format not implemented |
| GET | `/_node/{node}/_system` | **Yes** | Returns memory and goroutine statistics |
| POST | `/_node/{node}/_restart` | **Yes** | Returns `{"ok": true}`; no-op for embedded server |
| GET | `/_node/{node}/_versions` | **Yes** | Returns component version info |
| GET | `/_node/{node}/_smoosh/status` | **Yes** | Returns empty channels (no auto-compaction daemon) |

> **Note:** The legacy `/_config/...` API (pre-2.x) is also supported with the same handlers.

### Authentication

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| GET | `/_session` | **Yes** | Returns current session; supports cookie and basic auth; anonymous returns `name: null` |
| POST | `/_session` | **Yes** | Cookie login with `name`/`password`; accepts form-encoded and JSON bodies; JWT and proxy auth are unsupported extensions |
| DELETE | `/_session` | **Yes** | Clears session cookie; always returns `{"ok":true}` |

---

## Database API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| HEAD | `/{db}` | **Yes** | Checks database existence |
| GET | `/{db}` | **Yes** | Returns db info: doc count, update_seq, sizes |
| PUT | `/{db}` | **Yes** | Creates database; accepts `q`, `n`, `partitioned` query params (ignored in single-node mode) |
| DELETE | `/{db}` | **Yes** | |
| POST | `/{db}` | **Yes** | Creates document with auto-generated UUID |
| GET/POST | `/{db}/_all_docs` | **Yes** | Supports `skip`, `limit`, `startkey`/`start_key`, `endkey`/`end_key`, `key`, `inclusive_end`, `include_docs`, `keys` (POST body), `descending`, `update_seq`, `conflicts`, `attachments`, `att_encoding_info` |
| GET/POST | `/{db}/_design_docs` | **Yes** | Design-doc listing with POST `keys` body, `include_docs`, `update_seq`, and `_all_docs`-compatible query params |
| POST | `/{db}/_all_docs/queries` | **Yes** | Multi-query: accepts `queries` array, returns `results` array |
| POST | `/{db}/_design_docs/queries` | **Yes** | Multi-query for design docs |
| POST | `/{db}/_bulk_get` | **Yes** | Bulk document retrieval by ID/rev |
| PUT/POST | `/{db}/_bulk_docs` | **Partially** | Supports `docs`, `new_edits`; `new_edits=false` creates proper conflict leaves in `doc_leaves` bucket with CouchDB-compatible winner selection (highest generation, then lexicographic hash); per-document `error`/`reason` fields returned on conflict or not-found; missing `all_or_nothing` (deprecated) |
| POST | `/{db}/_find` | **Yes** | Supports `selector`, `limit`, `skip`, `bookmark`, `execution_stats`, `fields` projection, `sort` (asc/desc), `use_index` hint; equality conditions use Mango index when available; `r`, `q`, `conflicts`, `stable`, `update` accepted as single-node no-ops |
| POST | `/{db}/_index` | **Yes** | Creates Mango (json) index in a design document; returns `result=created` or `result=exists` |
| GET | `/{db}/_index` | **Yes** | Lists all Mango indexes plus built-in `_all_docs` special index |
| DELETE | `/{db}/_index/{ddoc}/json/{name}` | **Yes** | Deletes a named Mango index from the design document |
| POST | `/{db}/_explain` | **Yes** | Returns query plan with index, selector, opts |
| GET | `/{db}/_shards` | **Yes** | Single shard covering full range |
| GET | `/{db}/_shards/{docid}` | **Yes** | Returns single shard range and node |
| POST | `/{db}/_sync_shards` | **Yes** | No-op; returns `{"ok": true}` |
| GET/POST | `/{db}/_changes` | **Yes** | Supports feeds: `normal`, `longpoll`, `continuous`, `eventsource`; filters: `_doc_ids`, `_selector`, `_view`, design-doc filter functions; `since`, `limit`, `include_docs`, `heartbeat`, `timeout`, `descending`, `style=all_docs`, `seq_interval`, `conflicts`, `attachments`, `att_encoding_info` |
| POST | `/{db}/_compact` | **Yes** | Trims `RevHistory` in `docs` and `doc_leaves` buckets to `_revs_limit`, then rewrites the bbolt file via `bbolt.Compact` and atomically swaps it in |
| POST | `/{db}/_compact/{ddoc}` | **Partially** | Routed; triggers full-db compaction (bbolt has no per-view compaction) |
| POST | `/{db}/_ensure_full_commit` | **Yes** | No-op, returns `{"ok":true}` |
| POST | `/{db}/_view_cleanup` | **Partially** | Routed; returns `{"ok":true}` but is a no-op (bbolt has no stale view files to remove) |
| POST | `/{db}/_search_cleanup` | **Yes** | No-op; returns `{"ok": true}` |
| POST | `/{db}/_nouveau_cleanup` | **Yes** | No-op; returns `{"ok": true}` |
| GET | `/{db}/_security` | **Yes** | |
| PUT | `/{db}/_security` | **Yes** | |
| POST | `/{db}/_purge` | **Yes** | Accepts purge requests; echoes purged revisions (API-compatible, no tombstone removal) |
| GET | `/{db}/_purged_infos_limit` | **Yes** | Returns default limit of 1000 |
| PUT | `/{db}/_purged_infos_limit` | **Yes** | Accepts limit; no-op for embedded server |
| GET | `/{db}/_revs_limit` | **Yes** | |
| PUT | `/{db}/_revs_limit` | **Yes** | |
| POST | `/{db}/_missing_revs` | **Yes** | |
| POST | `/{db}/_revs_diff` | **Yes** | |

---

## Document API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| HEAD | `/{db}/{docid}` | **Yes** | Returns ETag; supports `rev` query param (checks specific revision); returns `X-Couch-Full-Commit` header |
| GET | `/{db}/{docid}` | **Yes** | Supports `rev`, `revs`, `conflicts`, `local_seq`, `latest`, `deleted_conflicts`, `meta`, `attachments` (inline base64), `att_encoding_info`, `multipart/mixed` accept header; `open_revs=all` and `open_revs=[...]` return all leaf revisions; `atts_since` accepted but not filtered |
| PUT | `/{db}/{docid}` | **Yes** | Supports JSON and `multipart/related`; inline base64 attachments; `_deleted` accepts boolean or string; `batch=ok` (returns 202 Accepted); `new_edits=false` (replication mode) |
| DELETE | `/{db}/{docid}` | **Yes** | Supports `rev` query param, `batch=ok` (returns 202 Accepted) |
| COPY | `/{db}/{docid}` | **Yes** | Copies source to destination specified in `Destination` header; supports `?rev=` on destination for overwrites |

### Attachment API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| HEAD | `/{db}/{docid}/{attname}` | **Yes** | Returns `ETag`, `Content-Type`, `Content-Length`; no body |
| GET | `/{db}/{docid}/{attname}` | **Yes** | Returns attachment binary with `ETag` and `Content-Length`; accepts `rev` query param; supports HTTP Range requests (206 Partial Content) |
| PUT | `/{db}/{docid}/{attname}` | **Yes** | Uploads attachment; enforces `rev`/`If-Match` conflict detection; returns `{"ok":true,"id":"...","rev":"..."}`; `batch=ok` (returns 202 Accepted) |
| DELETE | `/{db}/{docid}/{attname}` | **Yes** | Deletes attachment; enforces `rev`/`If-Match` conflict detection; returns `{"ok":true,"id":"...","rev":"..."}`; `batch=ok` (returns 202 Accepted) |

---

## Design Document API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| HEAD | `/{db}/_design/{ddoc}` | **Yes** | |
| GET | `/{db}/_design/{ddoc}` | **Yes** | |
| PUT | `/{db}/_design/{ddoc}` | **Yes** | |
| DELETE | `/{db}/_design/{ddoc}` | **Yes** | |
| COPY | `/{db}/_design/{ddoc}` | **Yes** | Copies source to destination specified in `Destination` header |
| HEAD | `/{db}/_design/{ddoc}/{attname}` | **Yes** | Returns `ETag`, `Content-Type`, `Content-Length`; no body |
| GET | `/{db}/_design/{ddoc}/{attname}` | **Yes** | Returns attachment binary with `ETag` and `Content-Length`; accepts `rev` query param; supports HTTP Range requests |
| PUT | `/{db}/_design/{ddoc}/{attname}` | **Yes** | Uploads attachment; enforces `rev`/`If-Match` conflict detection; `batch=ok`; returns full `id`/`rev` response |
| DELETE | `/{db}/_design/{ddoc}/{attname}` | **Yes** | Deletes attachment; enforces `rev`/`If-Match` conflict detection; `batch=ok`; returns full `id`/`rev` response |
| GET | `/{db}/_design/{ddoc}/_info` | **Yes** | Returns index info with `updater_running`, `waiting_clients`, `compact_running`, sizes, language |
| GET | `/{db}/_design/{ddoc}/_view/{view}` | **Yes** | Supports `skip`, `limit`, `include_docs`, `reduce`, `group`, `group_level`, `update`, `startkey`/`endkey`/`key`/`keys`, `inclusive_end`, `descending`, `stale`, `stable`, `sorted`, `update_seq`; accepts `startkey_docid`/`endkey_docid`, `attachments`, `att_encoding_info` |
| POST | `/{db}/_design/{ddoc}/_view/{view}` | **Yes** | Same as GET; accepts all query params as JSON body fields; `keys` array for multi-key lookup |
| POST | `/{db}/_design/{ddoc}/_view/{view}/queries` | **Yes** | Multi-query: accepts `queries` array, returns `results` array |
| GET/POST | `/{db}/_design/{ddoc}/_search/{index}` | **Yes** | Full CouchDB search API: `q` (Lucene query syntax: `AND`, `OR`, `NOT`, parentheses, `field:value`, `field:"phrase"`, wildcards, `*:*`), `limit`, `bookmark`, `sort`, `include_docs`, `include_fields`, `stale`, `counts` (term facets), `ranges` (numeric range facets), `drilldown`, `highlight_fields`/`highlight_pre_tag`/`highlight_post_tag`/`highlight_number`/`highlight_size`, `group_field`/`group_limit`/`group_sort`; POST accepts JSON body; backed by Bleve v2 full-text engine; grouping is post-processed (capped at 10k results); geo sort syntax not supported |
| GET | `/{db}/_design/{ddoc}/_search_info/{index}` | **Yes** | Returns basic search index info structure |
| POST | `/{db}/_design/{ddoc}/_nouveau/{index}` | **Yes** | Returns empty results (no full-text engine) |
| GET | `/{db}/_design/{ddoc}/_nouveau_info/{index}` | **Yes** | Returns basic index info structure |
| GET/POST | `/{db}/_design/{ddoc}/_show/{func}` | **Partially** | Returns 501 Not Implemented |
| GET/POST | `/{db}/_design/{ddoc}/_show/{func}/{docid}` | **Partially** | Returns 501 Not Implemented |
| GET/POST | `/{db}/_design/{ddoc}/_list/{func}/{view}` | **Partially** | Returns 501 Not Implemented |
| GET/POST | `/{db}/_design/{ddoc}/_list/{func}/{ddoc2}/{view}` | **Partially** | Returns 501 Not Implemented |
| POST | `/{db}/_design/{ddoc}/_update/{func}` | **Partially** | Returns 501 Not Implemented |
| PUT | `/{db}/_design/{ddoc}/_update/{func}/{docid}` | **Partially** | Returns 501 Not Implemented |
| ANY | `/{db}/_design/{ddoc}/_rewrite/{path}` | **Partially** | Returns 501 Not Implemented |

---

## Local Documents API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| GET/POST | `/{db}/_local_docs` | **Yes** | Supports `skip`, `limit`, `startkey`/`start_key`, `endkey`/`end_key`, `key`, `inclusive_end`, `descending`, `include_docs`, `update_seq`, `keys` (POST body); returns `total_rows: null`, `offset: null` per CouchDB spec; missing `conflicts` param |
| POST | `/{db}/_local_docs/queries` | **Yes** | Multi-query endpoint; accepts `{"queries": [...]}`, returns `{"results": [...]}` |
| GET | `/{db}/_local/{docid}` | **Yes** | Uses `0-N` revision scheme (no content hash) per CouchDB spec |
| PUT | `/{db}/_local/{docid}` | **Yes** | Uses `0-N` revision scheme; skips changes feed, views, and doc_leaves |
| DELETE | `/{db}/_local/{docid}` | **Yes** | |
| COPY | `/{db}/_local/{docid}` | **Yes** | Copies source to destination specified in `Destination` header |

---

## Partitioned Databases API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| GET | `/{db}/_partition/{partition}` | **Yes** | Returns partition info (doc_count, sizes) |
| GET | `/{db}/_partition/{partition}/_all_docs` | **Yes** | Lists docs scoped to partition key prefix |
| GET | `/{db}/_partition/{partition}/_design/{ddoc}/_view/{view}` | **Yes** | Delegates to view handler with partition doc ID bounds |
| POST | `/{db}/_partition/{partition}/_find` | **Yes** | Mango find filtered to partition |
| POST | `/{db}/_partition/{partition}/_explain` | **Yes** | Explain query plan for partition query |

---

## Summary

| Category | Yes | Partially | No |
|----------|-----|-----------|-----|
| Server | 17 | 0 | 2 |
| Cluster Setup | 5 | 0 | 2 |
| Node API | 9 | 0 | 1 |
| Authentication | 3 | 0 | 0 |
| Database | 24 | 3 | 0 |
| Document | 5 | 0 | 0 |
| Attachment | 4 | 0 | 0 |
| Design Document | 10 | 7 | 0 |
| Local Documents | 6 | 0 | 0 |
| Partitioned DBs | 5 | 0 | 0 |
| **Total** | **88** | **10** | **5** |

### Key capabilities present
- Full document CRUD with attachment support (inline base64, multipart/related, PUT/DELETE/HEAD/GET via `/{db}/{docid}/{attname}` and `/_design/{ddoc}/{attname}`)
- Replication protocol: `_changes`, `_revs_diff`, `_missing_revs`, `_bulk_docs` (with `new_edits:false`), `_local` checkpoint docs
- **Multi-revision conflict support**: `_bulk_docs` with `new_edits:false` stores concurrent leaf revisions in a `doc_leaves` bucket; winner chosen by CouchDB rule (highest generation, then lexicographic hash); `GET /{db}/{docid}` returns `_conflicts` field; `open_revs=all` / `open_revs=[...]` returns all leaf bodies; `_revs_diff` and `_missing_revs` recognise all conflict leaves as known
- Full-text search via `_search` endpoint: JavaScript and Tengo `index()` functions execute against a Bleve v2 backend; full CouchDB search API with Lucene syntax (`AND`, `OR`, `NOT`, parentheses, `field:value`, quoted phrases, wildcards), `bookmark` pagination, `sort`, `include_docs`, `counts`/`ranges` facets, `drilldown` filters, highlighting, `group_field` grouping, and POST body support
- Map/reduce views with reduce and grouping
- Mango `_find` with selector queries; `_index` CRUD (POST/GET/DELETE); equality conditions automatically use a matching Mango index when one exists
- Changes feed: normal, longpoll, continuous, eventsource — with `_doc_ids`, `_selector`, `_view`, and design-doc filter support
- Cookie-based session authentication with admin enforcement
- Runtime configuration via `/_config` and `/_node/{node}/_config`
- Per-database revision limit via `GET`/`PUT /{db}/_revs_limit` (stored in `meta` bucket; default 1000)
- Document compaction via `POST /{db}/_compact`: trims `RevHistory` in `docs` and `doc_leaves` buckets to `_revs_limit`, then rewrites the bbolt file to reclaim freed pages
- `POST /{db}/_all_docs` with `{"keys":[...]}` body

### Key gaps
- **Mango `_find`** index optimisation covers top-level equality conditions; range queries without an equality index still require a full-scan
- **Design doc functions**: show, list, update, rewrite return 501 Not Implemented (stubs present)
- **Nouveau search** returns stub responses (no real search engine)
- **Range requests** on attachments not supported
- Some design document view/search parameters only partially supported

### goydb Extensions

These features are deliberate deviations from CouchDB behaviour, designed to address real-world gaps.

#### `validate_on_replication` — VDU enforcement on replication writes

CouchDB's replication protocol uses `new_edits=false`, which bypasses `validate_doc_update` (VDU) functions. This is by design for trusted server-to-server replication, but it creates a security gap when untrusted clients (e.g. PouchDB in a browser) replicate directly to the server.

goydb adds two opt-in mechanisms to run VDU functions on replication writes:

| Mechanism | Scope | Default |
|-----------|-------|---------|
| `[couchdb] validate_on_replication` (config) | Global — all VDU functions run on replication writes | `false` |
| `"validate_on_replication": true` (design doc field) | Per-design-doc — only that VDU function runs on replication writes | not set |

**Behaviour:**
- When the global config is `true`, **all** VDU functions run on replication writes, regardless of per-design-doc settings.
- When the global config is `false` (default), only design documents that explicitly set `"validate_on_replication": true` run their VDU function on replication writes.
- Normal writes (`new_edits=true`) are unaffected — VDU functions always run as usual.

**Use case:** PouchDB or other untrusted client replication where you want server-side document validation.

**Warning:** Enabling this may break server-to-server replication if VDU rules on the target reject documents that the source considers valid. Use with care in multi-master setups.
