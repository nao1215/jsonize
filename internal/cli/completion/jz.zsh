#compdef jz
# zsh completion for jz (jsonize).
#
# Load it after compinit, or add the line to ~/.zshrc:
#
#   source <(jz completion zsh)
#
# or save it as _jz in a directory on $fpath. Every candidate comes from
# `jz __complete`, which reads the registries jz reads (the built-in one,
# your own and JSONIZE_REGISTRY_PATH) and runs no command.
_jz() {
    local -a lines
    lines=("${(@f)$(command ${words[1]} __complete "${(@)words[2,CURRENT]}" 2>/dev/null)}")
    case ${lines[1]} in
    words)
        (( ${#lines} > 1 )) && compadd -- "${(@)lines[2,-1]}"
        ;;
    files)
        _files
        ;;
    dirs)
        _files -/
        ;;
    esac
}

if [[ ${zsh_eval_context[-1]} == loadautofunc ]]; then
    _jz "$@"
else
    compdef _jz jz
fi
