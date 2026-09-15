# Recording for the website: `make demo`.
# jz new: strings, JSON values, an array, and a file's contents.
Output demo/new.gif

Set Shell bash
Set FontSize 18
Set Width 1200
Set Height 460
Set Padding 20
Set TypingSpeed 45ms
Set Theme "Dracula"

Hide
Type "export PS1='$ ' PATH=$PWD/dist:$PATH LC_ALL=C; cd $(mktemp -d)" Enter
Type "echo 1.4.0 > VERSION" Enter
Type "clear" Enter
Show

Type "jz new name=api replicas:=3 debug:=false tags[]=web tags[]=prod" Sleep 400ms Enter
Sleep 2s

Type "jz new version=@VERSION built_at=2026-09-15" Sleep 400ms Enter
Sleep 2s

Type "jz new --array web :=8080 :=true" Sleep 400ms Enter
Sleep 2s

# A key given twice is refused on purpose: it needs [] to be an array.
Type "jz new a=1 a=2; echo exit=$?" Sleep 400ms Enter
Sleep 3s
