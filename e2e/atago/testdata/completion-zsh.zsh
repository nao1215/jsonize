# Calls the zsh completion function the way the completion system does,
# with words and CURRENT set, and compadd and _files replaced by
# stand-ins that record what they were given.
autoload -Uz compinit && compinit -u -d "${TMPDIR:-/tmp}/jz-zcompdump-$$"
source <(jz completion zsh)
print -r -- "registered: ${_comps[jz]}"
typeset -a offered
compadd() { [[ $1 == -- ]] && shift; offered+=("$@") }
_files() { offered+=("<files${1:+ $1}>") }
t() {
    words=("$@")
    CURRENT=$#words
    offered=()
    _jz
    print -r -- "[${(j: :)words[2,-1]}] [${(j:|:)offered}]"
}
t jz ''
t jz --par
t jz --parser cur
t jz --parser=cur
t jz --parser curl --variant ''
t jz --parser=stat --variant=bsd
t jz list ping ''
t jz completion ''
t jz -f ''
t jz test ''
t jz --parser e2e
t jz run ./bin/e2etool ''
t jz --define ''
