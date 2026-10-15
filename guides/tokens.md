# Tokens and permissions

## What a management token can do

A management token acts for the person who created it, inside one organization.
It can do an action only when **both** of these allow it:

- the person's own role, and
- the permissions chosen when the token was created.

So an administrator's token with only `usage:read` can read usage and nothing
else. Run this to see what your token can do:

```
cchp account permissions
```

## When cchp says "Your token can't do this"

The message names the permission the action needs, for example
`provider:update`. Ask an administrator for a new token that includes it.
A token's permissions cannot be changed after it is created.

## Actions that need the browser

Some actions only work in the console:

- changing your own password, email, passkeys or two-step verification;
- showing a saved secret again, such as an LLM token or a provider key;
- rotating a management token.

cchp tells you "This server needs you to sign in from the browser for this
action." when you reach one. `cchp commands --all` lists them with the reason.

## Human verification

An administrator can require a human check (a short puzzle in the browser) for
some actions, such as creating tokens. cchp cannot complete it and never tries
to skip it. You'll see "This server needs you to complete a human check in the
browser for this action." Do the action in the console instead.

## Keeping tokens safe

- cchp never accepts a token as a command-line argument, because other programs
  on the computer can see arguments.
- Scripts pass it with `CCHP_TOKEN`, `CCHP_TOKEN_FILE` (a file only you can
  read) or `cchp login --token-stdin`, always together with `CCHP_BASE_URL`.
- cchp only sends the token to the server it was saved for.
