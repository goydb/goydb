"use strict";

const PouchDB = require("pouchdb");
PouchDB.plugin(require("pouchdb-find"));
PouchDB.plugin(require("pouchdb-adapter-memory"));

const GOYDB_URL = process.env.GOYDB_URL;
const GOYDB_USER = process.env.GOYDB_USER || "admin";
const GOYDB_PASS = process.env.GOYDB_PASS || "secret";
if (!GOYDB_URL) {
  console.error("GOYDB_URL environment variable is required");
  process.exit(1);
}

const AUTH_HEADER =
  "Basic " + Buffer.from(`${GOYDB_USER}:${GOYDB_PASS}`).toString("base64");

let passed = 0;
let failed = 0;

function ok(name) {
  passed++;
  console.log(`  \u2713 ${name}`);
}

function fail(name, err) {
  failed++;
  console.error(`  \u2717 ${name}`);
  console.error(`    ${err}`);
}

async function runTest(name, fn) {
  try {
    await fn();
    ok(name);
  } catch (err) {
    fail(name, err.message || err);
  }
}

function assert(condition, message) {
  if (!condition) throw new Error(message || "assertion failed");
}

// withTimeout wraps a promise with a timeout
function withTimeout(promise, ms, label) {
  return Promise.race([
    promise,
    new Promise((_, reject) =>
      setTimeout(() => reject(new Error(`${label}: timed out after ${ms}ms`)), ms)
    ),
  ]);
}

function assertEqual(actual, expected, message) {
  if (actual !== expected) {
    throw new Error(
      (message || "assertEqual") +
        `: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`
    );
  }
}

// Helper: create a database via PUT (bypassing PouchDB's auto-setup)
async function createDB(name) {
  const url = `${GOYDB_URL}/${name}`;
  const res = await fetch(url, {
    method: "PUT",
    headers: { Authorization: AUTH_HEADER },
  });
  if (!res.ok && res.status !== 412) {
    throw new Error(`PUT ${name} failed: ${res.status} ${await res.text()}`);
  }
}

// PouchDB ajax options for auth
const POUCH_OPTS = {
  skip_setup: true,
  auth: { username: GOYDB_USER, password: GOYDB_PASS },
};

// Helper to create a remote PouchDB pointing at goydb
function remoteDB(name) {
  return new PouchDB(`${GOYDB_URL}/${name}`, POUCH_OPTS);
}

async function main() {
  console.log(
    `PouchDB compatibility tests against ${GOYDB_URL.replace(/\/\/.*@/, "//***@")}`
  );
  console.log();

  // ── Database creation ──────────────────────────────────────────────
  await runTest("Database creation", async () => {
    await createDB("testdb");
    const db = remoteDB("testdb");
    const info = await db.info();
    assert(info.db_name === "testdb", `unexpected db_name: ${info.db_name}`);
  });

  // ── Document CRUD ──────────────────────────────────────────────────
  await runTest("Document CRUD", async () => {
    const db = remoteDB("testdb");

    // Create
    const putRes = await db.put({ _id: "doc1", title: "Hello", value: 42 });
    assert(putRes.ok, "put should succeed");
    assert(
      putRes.rev.startsWith("1-"),
      `rev should start with 1-, got ${putRes.rev}`
    );

    // Read
    const doc = await db.get("doc1");
    assertEqual(doc.title, "Hello");
    assertEqual(doc.value, 42);

    // Update
    doc.title = "Updated";
    const updateRes = await db.put(doc);
    assert(
      updateRes.rev.startsWith("2-"),
      `updated rev should start with 2-, got ${updateRes.rev}`
    );

    // Read again
    const updated = await db.get("doc1");
    assertEqual(updated.title, "Updated");

    // Delete
    const delRes = await db.remove(updated);
    assert(delRes.ok, "remove should succeed");
  });

  // ── Bulk operations ────────────────────────────────────────────────
  await runTest("Bulk operations", async () => {
    const db = remoteDB("testdb");

    const docs = [];
    for (let i = 0; i < 10; i++) {
      docs.push({ _id: `bulk_${i}`, index: i, type: "bulk_test" });
    }
    const bulkRes = await db.bulkDocs(docs);
    assertEqual(bulkRes.length, 10, "bulkDocs should return 10 results");
    for (const r of bulkRes) {
      assert(r.ok, `bulk doc ${r.id} should succeed`);
    }

    // allDocs
    const allRes = await db.allDocs({
      startkey: "bulk_0",
      endkey: "bulk_9",
      include_docs: true,
    });
    assertEqual(allRes.rows.length, 10, "allDocs should return 10 rows");
    assertEqual(allRes.rows[0].doc.index, 0);
  });

  // ── Attachments ────────────────────────────────────────────────────
  await runTest("Attachments", async () => {
    const db = remoteDB("testdb");

    // Put a document with inline attachment
    await db.put({
      _id: "with_attachment",
      _attachments: {
        "hello.txt": {
          content_type: "text/plain",
          data: Buffer.from("Hello, goydb!").toString("base64"),
        },
      },
    });

    // Retrieve and verify attachment
    const blob = await db.getAttachment("with_attachment", "hello.txt");
    // In Node.js PouchDB returns a Buffer
    const text = Buffer.isBuffer(blob)
      ? blob.toString("utf-8")
      : blob.toString();
    assertEqual(text, "Hello, goydb!", "attachment content mismatch");
  });

  // ── Design doc and view query ──────────────────────────────────────
  await runTest("Design doc and view query", async () => {
    const db = remoteDB("testdb");

    // Create design doc
    await db.put({
      _id: "_design/test",
      views: {
        by_type: {
          map: "function(doc) { if (doc.type) emit(doc.type, 1); }",
          reduce: "_count",
        },
      },
    });

    // Query with reduce
    const res = await db.query("test/by_type", {
      key: "bulk_test",
      reduce: true,
    });
    assert(res.rows.length > 0, "view should return rows");
    assertEqual(res.rows[0].value, 10, "count should be 10");

    // Query without reduce
    const mapRes = await db.query("test/by_type", {
      key: "bulk_test",
      reduce: false,
      include_docs: false,
    });
    assertEqual(mapRes.rows.length, 10);
  });

  // ── Mango find ─────────────────────────────────────────────────────
  await runTest("Mango find", async () => {
    const db = remoteDB("testdb");

    // Create index
    await db.createIndex({
      index: { fields: ["type", "index"] },
    });

    // Find with selector
    const res = await db.find({
      selector: { type: "bulk_test", index: { $gte: 5 } },
      sort: [{ index: "asc" }],
    });
    assertEqual(res.docs.length, 5, "find should return 5 docs (index 5-9)");
    assertEqual(res.docs[0].index, 5);
  });

  // ── Changes feed ───────────────────────────────────────────────────
  await runTest("Changes feed", async () => {
    const db = remoteDB("testdb");

    const changes = await db.changes({
      since: 0,
      include_docs: true,
      limit: 5,
    });
    assert(changes.results.length > 0, "changes should return results");
    assert(changes.results[0].doc, "changes should include docs");
    assert(changes.last_seq, "changes should have last_seq");
  });

  // ── Replication (local -> remote) ──────────────────────────────────
  await runTest("Replication (local -> remote)", async () => {
    await createDB("rep_target");

    // Create local in-memory PouchDB
    const local = new PouchDB("local_rep_src", { adapter: "memory" });
    const remote = remoteDB("rep_target");

    // Add docs to local
    await local.bulkDocs([
      { _id: "rep1", data: "one" },
      { _id: "rep2", data: "two" },
      { _id: "rep3", data: "three" },
    ]);

    // Replicate to remote
    const result = await withTimeout(
      PouchDB.replicate(local, remote),
      30000,
      "local->remote replication"
    );
    assert(result.ok, "replication should succeed");
    assertEqual(result.docs_written, 3, "should replicate 3 docs");

    // Verify on remote
    const doc = await remote.get("rep2");
    assertEqual(doc.data, "two");

    await local.destroy();
  });

  // ── Replication (remote -> local) ──────────────────────────────────
  await runTest("Replication (remote -> local)", async () => {
    // Use a fresh database to avoid interference from previous replication
    await createDB("rep_source");
    const src = remoteDB("rep_source");
    await src.bulkDocs([
      { _id: "rs1", data: "alpha" },
      { _id: "rs2", data: "beta" },
      { _id: "rs3", data: "gamma" },
    ]);

    const local = new PouchDB("local_rep_dst", { adapter: "memory" });

    const result = await withTimeout(
      PouchDB.replicate(src, local),
      15000,
      "remote->local replication"
    );
    assert(result.ok, "replication should succeed");
    assertEqual(result.docs_written, 3, `should replicate 3, got ${result.docs_written}`);

    const doc = await local.get("rs1");
    assertEqual(doc.data, "alpha");

    await local.destroy();
  });

  // ── Conflict handling ──────────────────────────────────────────────
  await runTest("Conflict handling", async () => {
    await createDB("conflict_test");
    const db = remoteDB("conflict_test");

    // Create initial doc
    const res = await db.put({ _id: "conflict_doc", value: "original" });
    const rev1 = res.rev;

    // Write two conflicting revisions using bulk_docs with new_edits: false
    await db.bulkDocs(
      [
        {
          _id: "conflict_doc",
          _rev: "2-aaa",
          value: "branch_a",
          _revisions: { start: 2, ids: ["aaa", rev1.split("-")[1]] },
        },
        {
          _id: "conflict_doc",
          _rev: "2-bbb",
          value: "branch_b",
          _revisions: { start: 2, ids: ["bbb", rev1.split("-")[1]] },
        },
      ],
      { new_edits: false }
    );

    // Fetch with conflicts
    const doc = await db.get("conflict_doc", { conflicts: true });
    assert(
      doc._conflicts && doc._conflicts.length > 0,
      "should have conflicts"
    );
  });

  // ── Summary ────────────────────────────────────────────────────────
  console.log();
  console.log(
    `Results: ${passed} passed, ${failed} failed, ${passed + failed} total`
  );

  if (failed > 0) {
    process.exit(1);
  }
}

main().catch((err) => {
  console.error("Fatal error:", err);
  process.exit(2);
});
