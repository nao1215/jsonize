# Recording for the website: `make demo`.
# jz new: strings, JSON values, an array, literal text, a JSON Pointer
# and one document per record.
Output demo/new.gif

Set Shell bash
Set FontSize 18
Set Width 1200
Set Height 560
Set Padding 20
Set TypingSpeed 45ms
Set Theme "Dracula"

Hide
Type "export PS1='$ ' PATH=$PWD/dist:$PATH LC_ALL=C; cd $(mktemp -d)" Enter
Type "echo 1.4.0 > VERSION" Enter
Type "clear" Enter
Show

Type "jz new name=api replicas:=3 tags[]=web tags[]=prod" Sleep 400ms Enter
Sleep 2s

Type "MESSAGE='@here: deploy := done'" Sleep 300ms Enter
Type `jz new --string "message=$MESSAGE" version=@VERSION` Sleep 400ms Enter
Sleep 2500ms

Type "jz new --path /metadata/name=api --path /spec/replicas:=3" Sleep 400ms Enter
Sleep 2500ms

Type `printf '{"used":1}\n{"used":2}\n' | jz new --each host=web sample:=@-` Sleep 400ms Enter
Sleep 2500ms

# A location given twice is refused on purpose: nothing is overwritten.
Type "jz new --path /spec/replicas:=3 --path /spec/replicas:=4; echo exit=$?" Sleep 400ms Enter
Sleep 3s
