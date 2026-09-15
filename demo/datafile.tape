# Recording for the website: `make demo`.
# A CSV file and a YAML file read as the format their extension names,
# and piped YAML read with --format.
Output demo/datafile.gif

Set Shell bash
Set FontSize 18
Set Width 1200
Set Height 520
Set Padding 20
Set TypingSpeed 45ms
Set Theme "Dracula"

Hide
Type "export PS1='$ ' PATH=$PWD/dist:$PATH LC_ALL=C; cd $(mktemp -d)" Enter
Type `printf 'id,name,email\n1,alice,alice@example.com\n2,bob,bob@example.com\n' > users.csv` Enter
Type `printf 'name: api\nreplicas: 3\nimage:\n  repository: example/api\n  tag: "1.4"\n' > deploy.yaml` Enter
Type "clear" Enter
Show

Type "cat users.csv" Sleep 300ms Enter
Sleep 1s
Type "jz --file users.csv" Sleep 400ms Enter
Sleep 2s

Type "jz --file deploy.yaml --pretty" Sleep 400ms Enter
Sleep 2500ms

Type "cat deploy.yaml | jz --format yaml --extract image" Sleep 400ms Enter
Sleep 3s
