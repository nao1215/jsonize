# Types into an interactive zsh through a pseudo-terminal and presses
# Tab, so the completion system itself runs the script jz printed. Each
# zpty -r waits for the text the completion has to put on the line.
zmodload zsh/zpty
zpty z zsh -f -i
zpty -w z 'PS1="READY> "; autoload -Uz compinit; compinit -u -d "${TMPDIR:-/tmp}/jz-zpty-$$"; source <(jz completion zsh); zstyle ":completion:*" menu no'
zpty -r -m z out '*READY> *'
zpty -w -n z $'jz --parser cur\t'
zpty -r -m z out '*curl*' && print -r -- "parser: curl"
zpty -w -n z $' --var\t'
zpty -r -m z out '*--variant*' && print -r -- "option: --variant"
zpty -w -n z $' hea\t'
zpty -r -m z out '*headers*' && print -r -- "variant: headers"
zpty -d z
