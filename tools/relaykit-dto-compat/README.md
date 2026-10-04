# relaykit-dto-compat

Compares the local `dto` package against upstream relaykit's `relaykit/dto`
across four dimensions, so a routine bump of the pinned relaykit commit can be
re-checked mechanically instead of by hand.

## Why four dimensions

A field-name diff is the obvious comparison and the one a naive script does. It
misses three things that break just as badly:

| Dimension | What a name-only diff misses |
| --- | --- |
| JSON tags | Same field name, different serialized key or `omitempty`. Compiles fine; the wire format silently changes |
| Go types | Same field name, different type. Usually a compile break, and `omitempty` means different things for `string` vs `*string` |
| Embedded fields | `ast.Field.Names` is empty for an anonymous embedded field, so it is invisible to both the name diff and the type diff |
| Function signatures | A name-only method diff never compares parameters or return values |

`interface{}` and `any` are the same type spelled two ways, so they are
reported in a separate "spelling" group rather than counted as differences.
The same goes for a spelling-only signature change.

## Usage

```sh
scripts/relaykit-dto-compat.sh              # human-readable report
scripts/relaykit-dto-compat.sh --markdown   # for pasting into a review record
scripts/relaykit-dto-compat.sh --strict     # exit non-zero if the counts drift
```

The wrapper flattens both trees from git (local from `HEAD`, relaykit from
`upstream/main:relaykit/dto`) into temp dirs and runs this tool against them.
Local is read from `HEAD`, never the worktree: other sessions edit this checkout
concurrently, and a worktree read would move the numbers underneath the
comparison.

Override the refs with environment variables:

```sh
LOCAL_REF=HEAD KIT_REF=upstream/main KIT_DIR=relaykit/dto scripts/relaykit-dto-compat.sh
```

## The `--strict` baseline

`--strict` compares the counts against the expectation recorded in
`main.go`, which is a point-in-time snapshot (the 15th review round). When
either side changes — another session commits to `dto`, or the pinned relaykit
commit moves — `--strict` fails until the baseline is re-recorded on purpose.
That is the intent: it is a gate against silent drift, not a permanent
invariant. Re-record it only after reading the diff the report prints.

## Direct use

```sh
cd tools/relaykit-dto-compat
go run . -local /path/to/local-dto -kit /path/to/relaykit-dto [-format markdown] [-strict]
```

Each directory holds the flattened non-test `.go` sources of one side.

## Tests

```sh
cd tools/relaykit-dto-compat && go test ./...
```

The tests cover each dimension with a minimal pair, including the case that is
easy to get wrong: an embedded field present on one side only.
