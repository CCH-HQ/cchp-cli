# Getting started

## 1. Get a management token

Open the CCHP console and go to **Account › Security › Management tokens**.
Create a token and choose what it may do. Copy it: the console shows it once.

A management token starts with `cch_auth_live_`. LLM tokens (`cch_llm_live_`)
are for calling models and cannot sign in to cchp.

## 2. Sign in

```
cchp login
```

cchp asks for the server address, for example `https://hub.example.com`, and the
token. It checks both with the server before saving anything. The token goes to
your system keychain, or to a private file when the computer has no keychain.

## 3. Look around

```
cchp                 # the home screen: usage, providers, keys, tokens, requests
cchp status          # which server and user you're signed in as
cchp usage summary   # today's, this week's and this month's figures
cchp providers list  # your providers
```

Every command explains itself with `--help`. To find one, search:

```
cchp commands --search "provider keys"
```

## 4. Sign out

```
cchp logout
```

This removes the token from this computer. Revoke it in the console if you no
longer need it anywhere.
