# Signing in: the server address, then the token (masked), checked before saving.
Output docs/media/login.gif
Require cchp

Set Shell "bash"
Set FontFamily "Liberation Mono"
Set FontSize 16
Set Width 1200
Set Height 640
Set Padding 20
Set TypingSpeed 50ms

Hide
Type "source {{ENV}} && clear"
Enter
Show

Type "cchp"
Sleep 300ms
Enter
Sleep 1500ms
Screenshot docs/media/login-signed-out.png
Type "q"
Sleep 500ms

Type "cchp login"
Sleep 300ms
Enter
Sleep 1200ms
Backspace 8
Sleep 300ms
Type "http://hub.example.com"
Sleep 1s
Screenshot docs/media/login-address-check.png
Backspace 22
Type "{{SERVER}}"
Sleep 1s
Screenshot docs/media/login-address.png
Enter
Sleep 800ms
Set TypingSpeed 5ms
Type "{{TOKEN}}"
Set TypingSpeed 50ms
Sleep 800ms
Screenshot docs/media/login-token.png
Enter
Sleep 3s
Screenshot docs/media/login-done.png

Type "cchp status"
Sleep 300ms
Enter
Sleep 2500ms
Screenshot docs/media/login-status.png
Sleep 1s
