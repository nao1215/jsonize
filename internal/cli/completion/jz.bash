# bash completion for jz (jsonize).
#
# Load it for the current shell, or add the line to ~/.bashrc:
#
#   source <(jz completion bash)
#
# Every candidate comes from `jz __complete`, which reads the registries
# jz reads (the built-in one, your own and JSONIZE_REGISTRY_PATH) and
# runs no command. Written for bash 3.2 and later.
_jz() {
    local cur=${COMP_WORDS[COMP_CWORD]}
    local prefix=
    local -a args
    args=("${COMP_WORDS[@]:1:COMP_CWORD}")
    # bash splits --opt=value at the "=", and the "=" is then the word
    # being completed when nothing follows it yet.
    if [ "$cur" = "=" ]; then
        prefix="="
        cur=
        args[${#args[@]}]=
    fi
    local out
    out=$(command "${COMP_WORDS[0]}" __complete "${args[@]}" 2>/dev/null) || return 0
    local kind=${out%%$'\n'*}
    local IFS=$'\n'
    COMPREPLY=()
    case $kind in
    words)
        local word first=1
        while read -r word; do
            if [ -n "$first" ]; then
                first=
                continue
            fi
            COMPREPLY[${#COMPREPLY[@]}]=$prefix$word
        done <<<"$out"
        ;;
    files)
        type compopt >/dev/null 2>&1 && compopt -o filenames 2>/dev/null
        COMPREPLY=($(compgen -f -- "$cur"))
        ;;
    dirs)
        type compopt >/dev/null 2>&1 && compopt -o filenames 2>/dev/null
        COMPREPLY=($(compgen -d -- "$cur"))
        ;;
    esac
    return 0
}
complete -F _jz jz
