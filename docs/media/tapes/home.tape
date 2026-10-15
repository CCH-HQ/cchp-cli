# The home screen: usage, providers, provider keys, LLM tokens and recent requests.
Output docs/media/home.gif
Require cchp

Set Shell "bash"
Set FontFamily "Liberation Mono"
Set FontSize 16
Set Width 1200
Set Height 760
Set Padding 20
Set TypingSpeed 60ms

Hide
Type "source {{ENV}} && clear"
Enter
Show

Type "cchp"
Sleep 400ms
Enter
Sleep 2s
Screenshot docs/media/home-menu.png

# Usage
Enter
Sleep 2s
Screenshot docs/media/home-usage.png
Escape
Sleep 500ms

# Providers, with search and a detail view
Down
Sleep 300ms
Enter
Sleep 2s
Screenshot docs/media/home-providers.png
Type "/"
Sleep 300ms
Type "mock"
Sleep 800ms
Enter
Sleep 400ms
Enter
Sleep 1s
Screenshot docs/media/home-provider-detail.png
Escape
Sleep 400ms
Escape
Sleep 400ms
Escape
Sleep 400ms

# Provider keys: pick a provider, then see its keys
Down
Sleep 300ms
Enter
Sleep 2s
Type "/"
Type "mock"
Sleep 600ms
Enter
Sleep 400ms
Enter
Sleep 2s
Screenshot docs/media/home-keys.png
Escape
Sleep 400ms
Escape
Sleep 400ms
Escape
Sleep 400ms

# LLM tokens
Down
Sleep 300ms
Enter
Sleep 2s
Screenshot docs/media/home-llm-tokens.png
Escape
Sleep 400ms

# Recent requests and one request's details
Down
Sleep 300ms
Enter
Sleep 2s
Screenshot docs/media/home-requests.png
Enter
Sleep 1s
Screenshot docs/media/home-request-detail.png
Escape
Sleep 400ms
Escape
Sleep 400ms
Type "q"
Sleep 800ms
