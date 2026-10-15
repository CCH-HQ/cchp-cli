# Errors

Each error says what happened and what to do. The request ID at the end helps
an administrator find the request in the server logs.

## You're not signed in

Run `cchp login`. Scripts set `CCHP_BASE_URL` with `CCHP_TOKEN` or
`CCHP_TOKEN_FILE`.

## The server didn't accept your token

The token was revoked, expired or copied wrongly. Create a new one in the
console and run `cchp login` again.

## Your token can't do this

The token lacks a permission the action needs; the message names it. Ask an
administrator for a token that includes it. See `cchp guide tokens`.

## This server needs you to sign in from the browser

The action only works in a signed-in browser. Use the console for it.

## This server needs you to complete a human check

An administrator turned on human verification for this action. Do it in the
console.

## The server rejected the input

A field is missing or has the wrong value. cchp lists each field, for example
`body.name`. Run `cchp schema <operationId>` to see the expected fields.

## Nothing was found there

The ID does not exist, or it belongs to another organization.

## The current state doesn't allow this

Something changed first, for example the item was already deleted. Load it
again and retry.

## The server is limiting requests

Wait a little and try again.

## No answer from the server

The connection broke or the time ran out. For a change, cchp cannot tell
whether it happened, so it never retries one by itself. Check the current state
first. When cchp printed an idempotency key, reusing it with
`--idempotency-key` lets the server recognize a repeat.

## The request was not sent

cchp could not reach the server, so nothing changed. Check the address and
your network, then try again.
