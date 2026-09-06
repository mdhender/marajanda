# Accounts Reference

## Identity and authentication

- An email address identifies an account.
- Email addresses are normalized to lowercase before lookup, comparison, or storage.
- Normalized email addresses are unique. Both the database and application behavior enforce uniqueness.
- Authentication uses email and password.
- Password hashes use bcrypt with `bcrypt.MinCost`.
- Account email addresses are the only personally identifiable information stored. Additional PII requires an explicit product decision.
- Every seated account has an immutable origin hex and map rotation. The main
  admin uses the game origin and rotation `0`; later player and admin accounts
  use deterministic placement. See [Player origin reference](reference/player-origin.md).
- An admin account is seated as it is created. A player account is created
  before it is seated and takes its origin hex when it configures its faction,
  because placement depends on the faction's race. An account that cannot be
  seated is refused, and no partially built account remains.

## Sign-in and sessions

The sign-in form labels the email identifier as `Account` and renders it as a text input. Inputs that are not syntactically email addresses are rejected before a database authentication lookup. All rejected credentials receive the same message.

Successful authentication creates a cryptographically random in-memory session. The browser receives the session identifier in a host-only `Secure`, `HttpOnly`, `SameSite=Lax` cookie without a persistent expiration. The session ends when the browser or server restarts.

Authenticated admins are directed to `/admin/dashboard`. Authenticated players are directed to `/player/dashboard`. Requests for either dashboard without a valid session are directed to `/sign-in`. The same role and session rules apply to the map pages described in [Map view reference](reference/map-view.md).

The player dashboard directs players with incomplete required faction metadata to `/player/faction`. The form takes a faction name and a race, chosen from the six the game knows and defaulting to `human`; an unrecognized race is rejected. Accepted faction configuration assigns the account's origin hex and directs the player back to `/player/dashboard`. A world with no valid hex left refuses the configuration and saves no faction.

Submitting `POST /sign-out` invalidates the current session, expires its cookie, and directs the browser to `/sign-in`.

### Development authentication

Non-production builds register `GET /__agents/log-me-in/{email}` when `ENV` is not `production`. The route finds the normalized account or creates a player account with a generated handle and secret, starts a normal browser session, and redirects to the safe same-origin path supplied by `returnTo`. Missing, absolute, protocol-relative, and malformed return paths redirect to `/`.

A player account reached through this route is given a randomly generated faction name and the default `human` race if it has no configured faction, so that the player dashboard does not divert the session to `/player/faction`. That also seats the account. An already configured faction is never replaced, and admin accounts receive no faction. The name is drawn from a passphrase generator, not from the game PRNG.

The `production` build tag omits the route. Setting `ENV=production` also prevents registration in a non-production build.

## Invitations

Registration is invitation-only.

An admin page accepts an email address and creates an invitation link for that address. Only one active invitation may exist for a normalized email address. An invitation expires 48 hours after issue.

Admins can delete or reissue invitations. Reissuing replaces or invalidates the previous invitation and never leaves multiple valid invitations for one email address.

## Registration

After following an invitation link, the invitee enters:

- Their email address to confirm the invitation. The page does not render or prefill the invited email address.
- A password.
- The password again as confirmation.

A password contains exactly eight characters, all of which are printable. Non-printing characters are rejected.

Registration verifies the invitation token, expiration, normalized email match, password confirmation, and account uniqueness before creating the account. Successful registration consumes or invalidates the invitation so that it cannot be reused.
