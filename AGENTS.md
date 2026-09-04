# Marajanda Agent Guide

## Product

Marajanda is an open-ended fantasy game with two mutually exclusive account roles:

- `admin`: manages the server and game and controls the single Marajanda faction, whose in-game powers are almost god-like.
- `player`: controls exactly one faction. Player factions begin with limited capabilities that increase through gameplay.

Do not give an account both roles or allow an account to control multiple factions. Ask the user before making game-rule or server-product decisions that are not specified here.

## Stack and architecture

- Implement the application in Go.
- Build server-rendered interactions with HTMX. Use Alpine.js only when HTMX alone is insufficient.
- Use ZombieZen SQLite for persistence and its SQL migration package for schema versions and migrations.
- Keep command functions thin. Commands may assemble dependencies and invoke behavior, but server and game-engine behavior must live in separate packages and must not be implemented in command functions.
- Before invoking a command through `github.com/peterbourgon/ff/v4`, load environment variables with `internal/dotenv`.
- Preserve the repository's existing copyright-header style in new Go files.

## Database contract

- Set SQLite `application_id` to the ASCII encoding of `MRJ0`: hexadecimal `0x4D524A30`, decimal `1297238576`.
- Enable write-ahead logging (WAL) and foreign-key enforcement on every database connection, including test and in-memory connections.
- Every open operation must migrate up. There is no migrate-down operation.
- Use ZombieZen's SQL migration version tracking. Do not create a custom migration-version table or versioning mechanism.
- Opening a database whose schema version is newer than the version supported by the application must fail.

Provide distinct open paths for:

1. A non-shared in-memory database. ZombieZen creates unique in-memory instances, so this requires its own open operation.
2. A named, shared in-memory database. This requires a separate open operation from the non-shared form.
3. A persistent database rooted at a caller-supplied directory.

Persistent open behavior is strict:

- Accept a directory path, not a database-file path.
- Always use the constant filename `marajanda.db` within that directory.
- Never create the directory or any missing parent directories.
- Fail if the supplied directory does not already exist or is not a directory.
- The server may create and migrate `marajanda.db` when the file itself does not exist.

Use in-memory databases for tests when practical.

## Server lifecycle and initial data

- The server requires a database directory path or the special value `:memory:`.
- For `:memory:`, create, migrate, and serve from an in-memory database. Seed these two accounts:
  - `admin@marajanda.com` / `good.luck` as an admin.
  - `player@marajanda.com` / `good.luck` as a player.
- Do not reject or warn about these known credentials: they belong to a temporary server instance.
- When the server creates a new persistent database, migrate it and seed the configured default admin account, normally sourced from an environment-specific local dotenv file.
- Do not reseed an existing persistent database merely because the server starts.
- Persistent database servers must shut down gracefully and close the database cleanly.

## Accounts and authentication

- Identify accounts by email address.
- Normalize every email address to lowercase before lookup, comparison, or storage.
- Enforce normalized email uniqueness in the database as well as in application behavior.
- Authenticate with email and password.
- Hash passwords with bcrypt using `bcrypt.MinCost`.
- PII consists only of account email addresses; do not introduce additional PII without asking.

## Invitations and registration

Registration is invitation-only.

- Give admins a page where they can enter an email address and create an invitation link for that address.
- Do not allow more than one active invitation for the same normalized email address.
- Invitation links expire 48 hours after issue.
- Allow admins to delete an invitation or reissue it. Reissuing replaces or invalidates the prior invitation; it must not leave multiple valid invitations for one email.
- After following an invitation link, the invitee must enter their email address to confirm it. Do not render or prefill the invited email address on the page.
- The invitee must enter and confirm their password.
- Require a password of eight characters, all of which are printable; reject non-printing characters.
- Registration must verify the invitation token, expiry, normalized email match, password confirmation, and uniqueness before creating the account.
- Consume or invalidate an invitation after successful registration so it cannot be reused.

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
