# Marajanda Agent Guide

## Required reading

Consult the document that owns the area you change:

- [Product](docs/PRODUCT.md): game roles, factions, and product constraints.
- [Architecture](docs/ARCHITECTURE.md): stack, package boundaries, command startup, and server lifecycle.
- [Datastore](docs/DATASTORE.md): SQLite identity, connection modes, migrations, and initial data.
- [Accounts](docs/ACCOUNTS.md): identity, authentication, invitations, and registration.

Ask the user before deciding game rules or server behavior that these documents do not specify.

## Work prioritization

- Implement bug fixes before new features.
- Before starting feature work, check the upstream repository for open bug issues.
- If any upstream bug issue is open, push back on the feature request and prioritize resolving the open bugs first.

## Coding directives

- Follow the documented contracts; do not silently weaken or reinterpret them.
- Keep command functions thin and keep server and game-engine code separate.
- Load environment variables with `internal/dotenv` before invoking commands through `github.com/peterbourgon/ff/v4`.
- Prefer HTMX for web interactions. Add Alpine.js only when HTMX is insufficient.
- Preserve the repository's existing copyright-header style in new Go files.

## Documentation directives

- All user-facing documentation must follow the Diataxis standard and be clearly structured as reference, how-to, explanation, or tutorial content.
- Load and follow the Diataxis skill when creating, reviewing, or restructuring user-facing documentation.

## Verification

- Run `go test ./...` for Go changes, plus focused package tests while iterating.
- Test database behavior with both non-shared and named shared in-memory opens where connection-sharing semantics matter.
- Cover persistent-open failures, migration on open, newer-schema rejection, WAL, foreign keys, normalization and uniqueness, invitation expiry/reissue/deletion/consumption, password validation, seed behavior, and graceful shutdown as those features are implemented.
- For UI changes, exercise the affected HTMX flow in a running server and inspect the rendered result.

## Beta version-control workflow

While `version.go` identifies the project as beta:

- Work directly on `main`; do not create or use feature branches.
- Commit completed changes and push them to `origin/main`.
- Every commit containing a code change must also bump either the minor or patch version in `version.go`, following semantic-versioning rules. Include the version bump in the same commit as the code change.
- Do not bump the version for documentation-only changes.
