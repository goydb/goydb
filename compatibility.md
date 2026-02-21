# goydb CouchDB API Compatibility Matrix

Based on the [CouchDB 3.x API reference](https://docs.couchdb.org/en/stable/api/index.html).

Legend: **Yes** = fully implemented · **Partially** = implemented with gaps (see details) · **No** = not implemented

---

## Server API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| GET | `/` | **Yes** | Returns welcome message, version, features |
| GET | `/_active_tasks` | **Partially** | Returns task list; only covers view-indexing tasks, no compaction or replication task types |
| GET | `/_all_dbs` | **Partially** | Lists databases; missing `startkey`, `endkey`, `limit`, `skip`, `descending` query params |
| POST | `/_dbs_info` | **No** | |
| GET | `/_db_updates` | **No** | |
| GET | `/_membership` | **Yes** | Returns single-node membership |
| POST | `/_replicate` | **Partially** | Supports `source`, `target`, `continuous`, `create_target`; missing `filter`, `query_params`, `doc_ids`, `selector`, `since_seq`, `use_checkpoints`, `checkpoint_interval`, `cancel` |
| GET | `/_scheduler/jobs` | **Partially** | Returns job list; basic structure only, no per-job history or state details |
| GET | `/_scheduler/docs` | **Partially** | Returns docs list; basic structure only |
| GET | `/_scheduler/docs/{replication-id}` | **No** | |
| POST | `/_search_analyze` | **No** | |
| POST | `/_nouveau_analyze` | **No** | |
| GET | `/_up` | **Yes** | Returns `{"status":"ok"}` |
| GET | `/_uuids` | **Yes** | Supports `count` query param |
| GET | `/_utils` | **No** | Fauxton web UI not served |
| GET | `/favicon.ico` | **No** | |

### Cluster Setup

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| GET | `/_cluster_setup` | **Partially** | Returns setup state; always reports single-node, no real cluster configuration |
| POST | `/_cluster_setup` | **Partially** | Accepts request; no-op for single-node, no cluster join/enable logic |
| GET | `/_reshard` | **No** | |
| GET | `/_reshard/state` | **No** | |
| PUT | `/_reshard/state` | **No** | |
| GET | `/_reshard/jobs` | **No** | |
| GET | `/_reshard/jobs/{jobid}` | **No** | |
| PUT | `/_reshard/jobs/{jobid}/state` | **No** | |

### Node API (`/_node/{node-name}/...`)

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| GET | `/_node/{node}` | **No** | |
| GET | `/_node/{node}/_config` | **Yes** | Node name is ignored; uses same config store |
| GET | `/_node/{node}/_config/{section}` | **Yes** | |
| GET | `/_node/{node}/_config/{section}/{key}` | **Yes** | |
| PUT | `/_node/{node}/_config/{section}/{key}` | **Yes** | Returns old value |
| DELETE | `/_node/{node}/_config/{section}/{key}` | **Yes** | |
| POST | `/_node/{node}/_config/_reload` | **No** | |
| GET | `/_node/{node}/_stats` | **No** | |
| GET | `/_node/{node}/_prometheus` | **No** | |
| GET | `/_node/{node}/_system` | **No** | |
| POST | `/_node/{node}/_restart` | **No** | |
| GET | `/_node/{node}/_versions` | **No** | |
| GET | `/_node/{node}/_smoosh/status` | **No** | |

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
| PUT | `/{db}` | **Partially** | Creates database; missing `q` (shards), `n` (replicas), `partitioned` query params |
| DELETE | `/{db}` | **Yes** | |
| POST | `/{db}` | **Yes** | Creates document with auto-generated UUID |
| GET/POST | `/{db}/_all_docs` | **Partially** | Supports `skip`, `limit`, `startkey`/`start_key`, `endkey`/`end_key`, `key`, `inclusive_end`, `include_docs`, `keys` (POST body); missing `descending`, `conflicts`, `update_seq`, `attachments`, `att_encoding_info` |
| GET/POST | `/{db}/_design_docs` | **Partially** | Basic design-doc listing; POST routed but keys body not handled; does not accept the same query params as `_all_docs` |
| POST | `/{db}/_all_docs/queries` | **No** | Multi-query not implemented |
| POST | `/{db}/_design_docs/queries` | **No** | |
| POST | `/{db}/_bulk_get` | **Yes** | Bulk document retrieval by ID/rev |
| PUT/POST | `/{db}/_bulk_docs` | **Partially** | Supports `docs`, `new_edits`; `new_edits=false` creates proper conflict leaves in `doc_leaves` bucket with CouchDB-compatible winner selection (highest generation, then lexicographic hash); per-document `error`/`reason` fields returned on conflict or not-found; missing `all_or_nothing` (deprecated) |
| POST | `/{db}/_find` | **Partially** | Supports `selector`, `limit`, `bookmark`, `execution_stats`; equality conditions use Mango index when available; missing `fields` projection, `sort`, `use_index`, `r`/`q` quorum params, `conflicts`, `stable`, `update` |
| POST | `/{db}/_index` | **Yes** | Creates Mango (json) index in a design document; returns `result=created` or `result=exists` |
| GET | `/{db}/_index` | **Yes** | Lists all Mango indexes plus built-in `_all_docs` special index |
| DELETE | `/{db}/_index/{ddoc}/json/{name}` | **Yes** | Deletes a named Mango index from the design document |
| POST | `/{db}/_explain` | **No** | Query plan explanation not implemented |
| GET | `/{db}/_shards` | **No** | |
| GET | `/{db}/_shards/{docid}` | **No** | |
| POST | `/{db}/_sync_shards` | **No** | |
| GET/POST | `/{db}/_changes` | **Partially** | Supports feeds: `normal`, `longpoll`, `continuous`, `eventsource`; filters: `_doc_ids`, `_selector`, `_view`, design-doc filter functions; `since`, `limit`, `include_docs`, `heartbeat`, `timeout`; missing `style=all_docs`, `descending`, `seq_interval`, `att_encoding_info`, `attachments`, `conflicts` |
| POST | `/{db}/_compact` | **Yes** | Trims `RevHistory` in `docs` and `doc_leaves` buckets to `_revs_limit`, then rewrites the bbolt file via `bbolt.Compact` and atomically swaps it in |
| POST | `/{db}/_compact/{ddoc}` | **Partially** | Routed; triggers full-db compaction (bbolt has no per-view compaction) |
| POST | `/{db}/_ensure_full_commit` | **Yes** | No-op, returns `{"ok":true}` |
| POST | `/{db}/_view_cleanup` | **Partially** | Routed; returns `{"ok":true}` but is a no-op (bbolt has no stale view files to remove) |
| POST | `/{db}/_search_cleanup` | **No** | |
| POST | `/{db}/_nouveau_cleanup` | **No** | |
| GET | `/{db}/_security` | **Yes** | |
| PUT | `/{db}/_security` | **Yes** | |
| POST | `/{db}/_purge` | **No** | |
| GET | `/{db}/_purged_infos_limit` | **No** | |
| PUT | `/{db}/_purged_infos_limit` | **No** | |
| GET | `/{db}/_revs_limit` | **Yes** | |
| PUT | `/{db}/_revs_limit` | **Yes** | |
| POST | `/{db}/_missing_revs` | **Yes** | |
| POST | `/{db}/_revs_diff` | **Yes** | |

---

## Document API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| HEAD | `/{db}/{docid}` | **Partially** | Returns ETag; missing `rev` query param support, `X-Couch-Full-Commit` header |
| GET | `/{db}/{docid}` | **Partially** | Supports `revs`, `local_seq`, `multipart/mixed` accept header; `open_revs=all` and `open_revs=[...]` return all leaf revisions; `_conflicts` field auto-populated when conflict branches exist; missing `rev` (fetch specific revision), `atts_since`, `att_encoding_info`, `attachments` (inline), `conflicts` query param (CouchDB gates `_conflicts` on this; goydb always includes it), `deleted_conflicts`, `meta`, `latest` |
| PUT | `/{db}/{docid}` | **Partially** | Supports JSON and `multipart/related`; inline base64 attachments; `_deleted` accepts boolean or string; missing `batch=ok`, `new_edits` query param, `X-Couch-Full-Commit` header |
| DELETE | `/{db}/{docid}` | **Partially** | Supports `rev` query param; missing `batch=ok` |
| COPY | `/{db}/{docid}` | **No** | `COPY` HTTP method not routed |

### Attachment API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| HEAD | `/{db}/{docid}/{attname}` | **Yes** | Returns `ETag`, `Content-Type`, `Content-Length`; no body |
| GET | `/{db}/{docid}/{attname}` | **Partially** | Returns attachment binary with `ETag` and `Content-Length`; missing `rev` query param, HTTP Range request support |
| PUT | `/{db}/{docid}/{attname}` | **Partially** | Uploads attachment; enforces `rev`/`If-Match` conflict detection; returns `{"ok":true,"id":"...","rev":"..."}`; missing `batch=ok` |
| DELETE | `/{db}/{docid}/{attname}` | **Partially** | Deletes attachment; enforces `rev`/`If-Match` conflict detection; returns `{"ok":true,"id":"...","rev":"..."}`; missing `batch=ok` |

---

## Design Document API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| HEAD | `/{db}/_design/{ddoc}` | **Yes** | |
| GET | `/{db}/_design/{ddoc}` | **Yes** | |
| PUT | `/{db}/_design/{ddoc}` | **Yes** | |
| DELETE | `/{db}/_design/{ddoc}` | **Yes** | |
| COPY | `/{db}/_design/{ddoc}` | **No** | `COPY` HTTP method not routed |
| HEAD | `/{db}/_design/{ddoc}/{attname}` | **Yes** | Returns `ETag`, `Content-Type`, `Content-Length`; no body |
| GET | `/{db}/_design/{ddoc}/{attname}` | **Partially** | Returns attachment binary with `ETag` and `Content-Length`; missing `rev` query param, HTTP Range request support |
| PUT | `/{db}/_design/{ddoc}/{attname}` | **Partially** | Uploads attachment; enforces `rev`/`If-Match` conflict detection; returns full `id`/`rev` response |
| DELETE | `/{db}/_design/{ddoc}/{attname}` | **Partially** | Deletes attachment; enforces `rev`/`If-Match` conflict detection; returns full `id`/`rev` response |
| GET | `/{db}/_design/{ddoc}/_info` | **Partially** | Returns index info; may lack `view_index.updater_running`, `waiting_clients`, `compact_running` fields |
| GET | `/{db}/_design/{ddoc}/_view/{view}` | **Partially** | Supports `skip`, `limit`, `include_docs`, `reduce`, `group`, `update`; missing POST method, `startkey`/`endkey`, `key`, `keys`, `descending`, `inclusive_end`, `conflicts`, `stable`, `update_seq`, `group_level`, `att_encoding_info`, `sorted` |
| POST | `/{db}/_design/{ddoc}/_view/{view}` | **No** | POST variant not routed |
| POST | `/{db}/_design/{ddoc}/_view/{view}/queries` | **No** | Multi-query not implemented |
| GET | `/{db}/_design/{ddoc}/_search/{index}` | **Partially** | Basic search with `q`, `limit`; missing `bookmark`, `counts`, `drilldown`, `group_field`, `group_limit`, `group_sort`, `highlight_fields`, `highlight_pre_tag`, `highlight_post_tag`, `highlight_number`, `highlight_size`, `include_docs`, `include_fields`, `ranges`, `sort`, `stale` |
| GET | `/{db}/_design/{ddoc}/_search_info/{index}` | **No** | |
| GET | `/{db}/_design/{ddoc}/_nouveau/{index}` | **No** | Nouveau (Lucene-based) search not implemented |
| GET | `/{db}/_design/{ddoc}/_nouveau_info/{index}` | **No** | |
| GET/POST | `/{db}/_design/{ddoc}/_show/{func}` | **No** | Show functions not implemented |
| GET/POST | `/{db}/_design/{ddoc}/_show/{func}/{docid}` | **No** | |
| GET/POST | `/{db}/_design/{ddoc}/_list/{func}/{view}` | **No** | List functions not implemented |
| GET/POST | `/{db}/_design/{ddoc}/_list/{func}/{ddoc2}/{view}` | **No** | |
| POST | `/{db}/_design/{ddoc}/_update/{func}` | **No** | Update functions not implemented |
| PUT | `/{db}/_design/{ddoc}/_update/{func}/{docid}` | **No** | |
| ANY | `/{db}/_design/{ddoc}/_rewrite/{path}` | **No** | URL rewriting not implemented |

---

## Local Documents API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| GET/POST | `/{db}/_local_docs` | **Partially** | Supports GET and POST (`{"keys":[...]}` body); missing `descending`, `conflicts`, `update_seq` params |
| POST | `/{db}/_local_docs/queries` | **No** | |
| GET | `/{db}/_local/{docid}` | **Yes** | |
| PUT | `/{db}/_local/{docid}` | **Yes** | |
| DELETE | `/{db}/_local/{docid}` | **Yes** | |
| COPY | `/{db}/_local/{docid}` | **No** | `COPY` HTTP method not routed |

---

## Partitioned Databases API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| GET | `/{db}/_partition/{partition}` | **No** | Partitioned databases not implemented |
| GET | `/{db}/_partition/{partition}/_all_docs` | **No** | |
| GET | `/{db}/_partition/{partition}/_design/{ddoc}/_view/{view}` | **No** | |
| POST | `/{db}/_partition/{partition}/_find` | **No** | |
| POST | `/{db}/_partition/{partition}/_explain` | **No** | |

---

## Summary

| Category | Yes | Partially | No |
|----------|-----|-----------|-----|
| Server | 4 | 7 | 14 |
| Cluster Setup | 0 | 2 | 6 |
| Node API | 5 | 0 | 8 |
| Authentication | 3 | 0 | 0 |
| Database | 10 | 7 | 11 |
| Document | 0 | 4 | 1 |
| Attachment | 1 | 3 | 0 |
| Design Document | 5 | 6 | 10 |
| Local Documents | 3 | 1 | 2 |
| Partitioned DBs | 0 | 0 | 5 |
| **Total** | **29** | **32** | **57** |

### Key capabilities present
- Full document CRUD with attachment support (inline base64, multipart/related, PUT/DELETE/HEAD/GET via `/{db}/{docid}/{attname}` and `/_design/{ddoc}/{attname}`)
- Replication protocol: `_changes`, `_revs_diff`, `_missing_revs`, `_bulk_docs` (with `new_edits:false`), `_local` checkpoint docs
- **Multi-revision conflict support**: `_bulk_docs` with `new_edits:false` stores concurrent leaf revisions in a `doc_leaves` bucket; winner chosen by CouchDB rule (highest generation, then lexicographic hash); `GET /{db}/{docid}` returns `_conflicts` field; `open_revs=all` / `open_revs=[...]` returns all leaf bodies; `_revs_diff` and `_missing_revs` recognise all conflict leaves as known
- Map/reduce views with reduce and grouping
- Mango `_find` with selector queries; `_index` CRUD (POST/GET/DELETE); equality conditions automatically use a matching Mango index when one exists
- Changes feed: normal, longpoll, continuous, eventsource — with `_doc_ids`, `_selector`, `_view`, and design-doc filter support
- Cookie-based session authentication with admin enforcement
- Runtime configuration via `/_config` and `/_node/{node}/_config`
- Per-database revision limit via `GET`/`PUT /{db}/_revs_limit` (stored in `meta` bucket; default 1000)
- Document compaction via `POST /{db}/_compact`: trims `RevHistory` in `docs` and `doc_leaves` buckets to `_revs_limit`, then rewrites the bbolt file to reclaim freed pages
- `POST /{db}/_all_docs` with `{"keys":[...]}` body

### Key gaps
- **COPY** method not implemented anywhere
- **Mango `_explain`** not implemented
- **Mango `_find`** index optimisation only covers top-level equality conditions; range queries, sort, and `use_index` still full-scan
- **Purge API** not implemented
- **Design doc functions**: show, list, update, rewrite not implemented
- **Partitioned databases** not supported
- **Range requests** on attachments not supported
- **`rev` query param** on GET `/{db}/{docid}` (fetching a specific old revision) not implemented — use `open_revs=[...]` to retrieve specific leaf revisions; non-leaf ancestors are not retrievable
- **`conflicts` query param** on GET `/{db}/{docid}` not gated — goydb always includes `_conflicts` when conflict branches exist, rather than requiring the param
- **Nouveau search** engine not implemented
- Node stats/system/prometheus endpoints not exposed
- `_dbs_info`, `_db_updates` endpoints missing
