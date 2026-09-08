# Our fail-closed permission gate did not fail closed

*2026-09-08*

Airlock's pitch has one load-bearing sentence: if a change widens what your agent
is allowed to do, the pull request stops until a human signs off. The README said
so. The Action said "fail-closed on permission expansion by default". The guide
had a section called "MCP approval demo" with numbered steps.

We ran those steps. The gate did not fire.

```console
$ cd testdata/toy-agent
$ airlock init && airlock snapshot
$ # add "write" under mcp.local-fs.permissions in apm.lock.yaml
$ airlock diff
╭─ airlock ────────────────────────────────────╮
│ base  ef1aad1044dff9ff                       │
│ head  working-c92d3be7ae35                   │
│                                              │
│ no AI artifact changes                       │
╰──────────────────────────────────────────────╯
$ airlock ci --fail-on-approval
$ echo $?
0
```

A filesystem MCP server gained write access. Airlock said nothing changed and
exited zero. On a real repository that is a green check mark on a pull request
that just handed an agent the ability to modify files.

## Why

Airlock hashes each AI artifact and compares snapshots. An MCP server's hash came
from one field:

```go
for _, x := range m.MCPServers {
    out = append(out, ArtifactRef{Kind: "mcp", ID: x.ID, Hash: x.SchemaHash})
}
```

`SchemaHash` describes the server's tool surface. For an HTTP server, Airlock
fetches `tools/list` and hashes what comes back, so that field genuinely moves
when the server changes. For a server declared in an APM lockfile, it is whatever
the lockfile pins:

```yaml
mcp:
  local-fs:
    name: local-fs
    hash: abc123mcpfixhash0001   # pinned literal
    permissions:
      - read
```

Editing `permissions:` does not touch `hash:`. So the artifact hash was identical
before and after, and no artifact was marked changed.

That is where it became invisible rather than merely wrong. The permission check
runs *over the list of changed artifacts*:

```go
for _, c := range changes {
    if c.Status != "added" && c.Status != "changed" {
        continue
    }
    switch c.Kind {
    case "mcp":
        // ... compare head permissions against base permissions
```

The comparison logic was correct and well tested. It compared the right fields
and produced the right reason strings. It was simply never reached, because
nothing upstream of it considered the artifact changed. Prove it by bumping the
pinned hash by hand and the gate fires perfectly:

```console
$ airlock diff
│   ~  mcp          local-fs             abc123mc -> abc123mc │
│   MCP new permission write on local-fs                      │
│   MCP permissions expanded: local-fs                        │
```

The permissions were even sitting right there in the manifest on disk, correctly
discovered and correctly written out:

```json
{"id": "local-fs", "schema_hash": "abc123mcpfixhash0001",
 "permissions": ["read", "write"], "source": "apm.lock.yaml"}
```

Read the whole file and the widening is obvious. Every individual piece worked.
The bug lived in the seam between them.

## The fix

An artifact's hash should cover everything that matters about it, and for an MCP
server the granted permissions matter as much as the tool surface:

```go
func mcpArtifactHash(x MCPServer) string {
	if len(x.Permissions) == 0 {
		return x.SchemaHash
	}
	perms := make([]string, len(x.Permissions))
	copy(perms, x.Permissions)
	sort.Strings(perms)
	return HashString(x.SchemaHash + "|perms:" + strings.Join(perms, ","))
}
```

Sorting matters. Without it, reordering two lines in a lockfile would report a
permission change, and a gate that cries wolf gets switched off. With it:

| Edit | Result |
|---|---|
| Add `write` | Artifact changed, `NEEDS_APPROVAL`, exit 1 |
| Reorder `read` and `write` | No change |
| Remove `write` | Artifact changed, no approval required |

That last row is deliberate. Narrowing permissions is a change worth recording in
the diff, but it is not an expansion, so it does not need a human.

## What we take from this

**A test that asserts on the reason string is not a test of the gate.** The
permission-comparison code had coverage. Every assertion passed. None of them
started from "edit a lockfile" and ended at "what is the exit code", so none of
them crossed the seam where the bug was. The regression test we added begins with
the manifest a lockfile actually produces and asks whether the hash moves.

**Fail-closed is a property of a path, not of a function.** You cannot inspect
the comparison in isolation and conclude anything about whether the release is
gated. The only claim worth making is about the whole path: lockfile edit in,
non-zero exit out. That is now the thing under test.

**Pinned hashes are load-bearing input.** A field a user can freeze is a field
that will be frozen, and anything derived only from it inherits that freeze.
Deriving identity from data the user pins, rather than from data we compute, meant
the security property was theirs to accidentally disable.

Fixed in [#8](https://github.com/xdlc-labs/airlock/pull/8), along with the tests
that would have caught it. If you were running an earlier beta and relying on MCP
permission gating, it was not gating. Sorry.
