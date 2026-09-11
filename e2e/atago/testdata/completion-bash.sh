# Drives the bash completion jz prints the way readline does: COMP_WORDS
# holds the words as bash splits them, "=" included, COMP_CWORD the one
# being completed, and _jz leaves the candidates in COMPREPLY. Each case
# prints the words and, sorted and joined by "|", what was offered.
# eval rather than source <(...): bash 3.2, the one macOS ships, reads
# nothing from a process substitution given to source.
eval "$(jz completion bash)"
complete -p jz
t() {
    COMP_WORDS=("$@")
    COMP_CWORD=$((${#COMP_WORDS[@]} - 1))
    COMPREPLY=()
    _jz
    local offered
    offered=$(printf '%s\n' "${COMPREPLY[@]}" | LC_ALL=C sort | paste -sd '|' -)
    printf '[%s] [%s]\n' "${*:2}" "$offered"
}
t jz ''
t jz --par
t jz --parser cur
t jz --parser = cur
# With nothing after the "=", the "=" is the word bash replaces, so every
# candidate carries it.
COMP_WORDS=(jz --parser =)
COMP_CWORD=2
_jz
bare=0
for c in "${COMPREPLY[@]}"; do case $c in =*) ;; *) bare=$((bare + 1)) ;; esac; done
case " ${COMPREPLY[*]} " in *" =curl "*) echo "[--parser =] [=curl among ${#COMPREPLY[@]}, $bare without the =]" ;; esac
t jz --parser curl --variant ''
t jz --parser stat --variant bsd
t jz run --parser csv --variant tab
t jz list ping ''
t jz list ping bsd ''
t jz completion ''
t jz -f files/fil
t jz --file files/
t jz run curl -s
t jz --parser e2e
t jz list e2etool ''
t jz run ./bin/e2etool files/f
t jz --define ''
