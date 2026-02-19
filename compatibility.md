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
| GET | `/_session` | **Partially** | Returns current session; supports cookie and basic auth only |
| POST | `/_session` | **Partially** | Cookie login with `name`/`password`; missing JWT, proxy auth, 2FA (`token` param) |
| DELETE | `/_session` | **Yes** | Clears session cookie |

---

## Database API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| HEAD | `/{db}` | **Yes** | Checks database existence |
| GET | `/{db}` | **Yes** | Returns db info: doc count, update_seq, sizes |
| PUT | `/{db}` | **Partially** | Creates database; missing `q` (shards), `n` (replicas), `partitioned` query params |
| DELETE | `/{db}` | **Yes** | |
| POST | `/{db}` | **Yes** | Creates document with auto-generated UUID |
| GET/POST | `/{db}/_all_docs` | **Partially** | Supports `skip`, `limit`, `startkey`/`start_key`, `endkey`/`end_key`, `include_docs`, `keys` (POST body); missing `key`, `descending`, `inclusive_end`, `conflicts`, `update_seq`, `attachments`, `att_encoding_info` |
| GET/POST | `/{db}/_design_docs` | **Partially** | Basic design-doc listing; POST routed but keys body not handled; does not accept the same query params as `_all_docs` |
| POST | `/{db}/_all_docs/queries` | **No** | Multi-query not implemented |
| POST | `/{db}/_design_docs/queries` | **No** | |
| POST | `/{db}/_bulk_get` | **Yes** | Bulk document retrieval by ID/rev |
| PUT/POST | `/{db}/_bulk_docs` | **Partially** | Supports `docs`, `new_edits`; missing `all_or_nothing` (deprecated), per-document error detail in response body (`error`/`reason` fields missing when write fails) |
| POST | `/{db}/_find` | **Partially** | Supports `selector`, `limit`, `bookmark`, `execution_stats`; missing `fields` projection, `sort`, `use_index`, `r`/`q` quorum params, `conflicts`, `stable`, `update` |
| POST | `/{db}/_index` | **No** | Mango index creation not implemented |
| GET | `/{db}/_index` | **No** | |
| DELETE | `/{db}/_index/{ddoc}/json/{name}` | **No** | |
| POST | `/{db}/_explain` | **No** | Query plan explanation not implemented |
| GET | `/{db}/_shards` | **No** | |
| GET | `/{db}/_shards/{docid}` | **No** | |
| POST | `/{db}/_sync_shards` | **No** | |
| GET/POST | `/{db}/_changes` | **Partially** | Supports feeds: `normal`, `longpoll`, `continuous`, `eventsource`; filters: `_doc_ids`, `_selector`, `_view`, design-doc filter functions; `since`, `limit`, `include_docs`, `heartbeat`, `timeout`; missing `style=all_docs`, `descending`, `seq_interval`, `att_encoding_info`, `attachments`, `conflicts` |
| POST | `/{db}/_compact` | **Yes** | Rewrites live data into a new file via `bbolt.Compact` and atomically swaps it in |
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
| GET | `/{db}/_revs_limit` | **No** | |
| PUT | `/{db}/_revs_limit` | **No** | |
| POST | `/{db}/_missing_revs` | **Yes** | |
| POST | `/{db}/_revs_diff` | **Yes** | |

---

## Document API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| HEAD | `/{db}/{docid}` | **Partially** | Returns ETag; missing `rev` query param support, `X-Couch-Full-Commit` header |
| GET | `/{db}/{docid}` | **Partially** | Supports `revs`, `local_seq`, `open_revs`, `multipart/mixed` accept header; missing `rev` (fetch specific revision), `atts_since`, `att_encoding_info`, `attachments` (inline), `conflicts`, `deleted_conflicts`, `meta`, `latest` |
| PUT | `/{db}/{docid}` | **Partially** | Supports JSON and `multipart/related`; inline base64 attachments; `_deleted` accepts boolean or string; missing `batch=ok`, `new_edits` query param, `X-Couch-Full-Commit` header |
| DELETE | `/{db}/{docid}` | **Partially** | Supports `rev` query param; missing `batch=ok` |
| COPY | `/{db}/{docid}` | **No** | `COPY` HTTP method not routed |

### Attachment API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| HEAD | `/{db}/{docid}/{attname}` | **No** | No HEAD handler for attachments |
| GET | `/{db}/{docid}/{attname}` | **Partially** | Returns attachment binary; missing `rev` query param, HTTP Range request support |
| PUT | `/{db}/{docid}/{attname}` | **Partially** | Uploads attachment; missing `rev` validation in query param |
| DELETE | `/{db}/{docid}/{attname}` | **Partially** | Deletes attachment; missing `rev` query param validation, `batch=ok` |

---

## Design Document API

| Method | Endpoint | Status | Notes |
|--------|----------|--------|-------|
| HEAD | `/{db}/_design/{ddoc}` | **No** | No HEAD handler for design documents |
| GET | `/{db}/_design/{ddoc}` | **Yes** | |
| PUT | `/{db}/_design/{ddoc}` | **Yes** | |
| DELETE | `/{db}/_design/{ddoc}` | **Yes** | |
| COPY | `/{db}/_design/{ddoc}` | **No** | `COPY` HTTP method not routed |
| HEAD | `/{db}/_design/{ddoc}/{attname}` | **No** | |
| GET | `/{db}/_design/{ddoc}/{attname}` | **No** | No route for design doc attachments |
| PUT | `/{db}/_design/{ddoc}/{attname}` | **No** | |
| DELETE | `/{db}/_design/{ddoc}/{attname}` | **No** | |
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
| GET | `/{db}/_local_docs` | **Partially** | GET only; missing POST method; missing `keys`, `descending`, `conflicts`, `update_seq` params |
| POST | `/{db}/_local_docs` | **No** | POST variant not routed |
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
| Authentication | 1 | 2 | 0 |
| Database | 8 | 7 | 13 |
| Document | 0 | 4 | 1 |
| Attachment | 0 | 3 | 1 |
| Design Document | 3 | 3 | 15 |
| Local Documents | 3 | 1 | 3 |
| Partitioned DBs | 0 | 0 | 5 |
| **Total** | **24** | **29** | **66** |

### Key capabilities present
- Full document CRUD with attachment support (inline base64, multipart/related)
- Replication protocol: `_changes`, `_revs_diff`, `_missing_revs`, `_bulk_docs` (with `new_edits:false`), `_local` checkpoint docs
- Map/reduce views with reduce and grouping
- Mango `_find` with selector queries
- Changes feed: normal, longpoll, continuous, eventsource — with `_doc_ids`, `_selector`, `_view`, and design-doc filter support
- Cookie-based session authentication with admin enforcement
- Runtime configuration via `/_config` and `/_node/{node}/_config`
- Real database compaction via `POST /{db}/_compact` (rewrites bbolt file, reclaims freed pages)
- `POST /{db}/_all_docs` with `{"keys":[...]}` body

### Key gaps
- **COPY** method not implemented anywhere
- **Mango indexes** (`_index`, `_explain`) not implemented — `_find` works but always full-scans
- **Purge API** not implemented
- **Revision limit** (`_revs_limit`) not implemented
- **Design doc functions**: show, list, update, rewrite not implemented
- **Partitioned databases** not supported
- **Range requests** on attachments not supported
- **`rev` query param** on GET `/{db}/{docid}` (fetching old revisions) not implemented — only current revision is accessible
- **Nouveau search** engine not implemented
- Node stats/system/prometheus endpoints not exposed
- `_dbs_info`, `_db_updates` endpoints missing
