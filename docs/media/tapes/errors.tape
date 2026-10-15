# Errors in plain words: a missing permission, a browser-only action, a typo.
Output docs/media/errors.gif
Require cchp

Set Shell "bash"
Set FontFamily "Liberation Mono"
Set FontSize 16
Set Width 1200
Set Height 560
Set Padding 20
Set TypingSpeed 50ms

Hide
Type "source {{ENV}} && clear"
Enter
Show

Type "cchp admin users"
Sleep 300ms
Enter
Sleep 2500ms

Type "cchp api getMFAStatus"
Sleep 300ms
Enter
Sleep 1500ms

Type "cchp providers lsit"
Sleep 300ms
Enter
Sleep 1500ms
Screenshot docs/media/errors.png
Sleep 1s
