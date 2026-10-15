# Finding a command, reading it, and a short guide.
Output docs/media/discover.gif
Require cchp

Set Shell "bash"
Set FontFamily "Liberation Mono"
Set FontSize 16
Set Width 1200
Set Height 760
Set Padding 20
Set TypingSpeed 50ms

Hide
Type "source {{ENV}} && clear"
Enter
Show

Type 'cchp commands --search "disable key"'
Sleep 300ms
Enter
Sleep 2s
Screenshot docs/media/discover-search.png
Type "clear"
Enter

Type "cchp commands show revokeProviderKey"
Sleep 300ms
Enter
Sleep 2s
Screenshot docs/media/discover-show.png
Type "clear"
Enter

Type "cchp keys list --help"
Sleep 300ms
Enter
Sleep 2s
Screenshot docs/media/discover-help.png
Sleep 1s
