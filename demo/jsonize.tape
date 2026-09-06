# Recording for the README and the website: `vhs demo/jsonize.tape`.
# It shows the three things that matter, in order: a pipe with nothing to
# configure, a second command read by the same invocation, and a refusal
# where the format is not evidence of anything.
Output demo/jsonize.gif

Set Shell bash
Set FontSize 18
Set Width 1200
Set Height 620
Set Padding 20
Set TypingSpeed 45ms
Set Theme "GitHub Dark"

Hide
Type "export PS1='$ ' PATH=$PWD/dist:$PATH LC_ALL=C" Enter
Type "clear" Enter
Show

Type "df -h | jz | head -c 200" Sleep 400ms Enter
Sleep 2s

Type "uptime | jz" Sleep 400ms Enter
Sleep 2s

Type "jz run --pretty free" Sleep 400ms Enter
Sleep 2500ms

# A numstat line has the shape of du output and nothing more, so jz says
# so instead of converting it into a plausible lie.
Type "printf '10\t2\tmain.go\n' | jz" Sleep 400ms Enter
Sleep 3s
