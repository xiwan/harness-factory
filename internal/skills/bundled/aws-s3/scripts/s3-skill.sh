#!/usr/bin/env bash
# s3-skill.sh — S3 skill distribution, artifact upload, and presigned URLs.
#
# Subcommands:
#   config [<s3-uri>]              show or persist the skill-distribution S3 URI
#   list                           list skills available in the bucket
#   pull [<name> ...]              download skills (all if no name given), validated + audited
#   push <skill-dir>               publish a local skill to the bucket (audited first)
#   upload <local-path> <s3-uri>   upload a file or directory (artifact delivery)
#   presign <s3-uri> [seconds]     generate a time-limited download URL
#
# Design notes:
#   - Staging happens OUTSIDE skills_dir. A crashed pull must never leave a
#     directory that the skill loader would pick up as a live, unaudited skill.
#   - Remote content is never made executable. Callers must run scripts
#     explicitly (`bash <path>`), which keeps them subject to shell allowlists.
#   - Bundled skill names are reserved: a hostile bucket must not be able to
#     shadow the audit gate or this script itself.
#   - Ownership is tracked in <skills_dir>/.s3-managed so updates are possible
#     without ever clobbering hand-written local skills.

set -uo pipefail

readonly SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SKILL_DIR="$(dirname "$SELF_DIR")"
readonly SKILLS_ROOT="$(dirname "$SKILL_DIR")"

readonly CONFIG_FILE="$SKILL_DIR/.s3-config"
readonly MANIFEST="$SKILLS_ROOT/.s3-managed"
readonly AUDIT="$SKILLS_ROOT/skill-security-audit/scripts/audit.sh"

# Names that may never be supplied by a remote bucket.
readonly RESERVED="aws-s3 skill-security-audit skill-creator game-qa game-design-coach"

readonly MAX_FILE_BYTES=$((64 * 1024))
readonly MAX_SKILL_FILES=100
readonly MAX_TOTAL_BYTES=$((10 * 1024 * 1024))
readonly MAX_PRESIGN_SECONDS=604800 # S3 SigV4 hard limit: 7 days

# Populated by cmd_pull; declared here so the EXIT trap can reference it safely
# even when pull was never reached.
staging=""

die() { echo "ERROR: $*" >&2; exit 1; }
note() { echo "$*"; }

# ---------------------------------------------------------------- preflight

require_deps() {
    command -v aws >/dev/null 2>&1 || die "aws CLI not found in PATH. Install it or ask the operator to."
    command -v jq  >/dev/null 2>&1 || die "jq not found in PATH. Required to read audit results."
    aws sts get-caller-identity >/dev/null 2>&1 \
        || die "no usable AWS credentials (checked instance role / environment). Cannot reach S3."
}

# Region is optional: aws CLI resolves it from config/instance metadata otherwise.
aws_region_args() {
    if [[ -n "${AWS_REGION:-}${AWS_DEFAULT_REGION:-}" ]]; then
        printf '%s' ""
    fi
}

# ---------------------------------------------------------------- s3 uri

normalize_uri() {
    local uri="$1"
    [[ "$uri" == s3://* ]] || die "not an S3 URI: $uri (expected s3://bucket/prefix)"
    printf '%s' "${uri%/}"
}

# Resolution order: HF_SKILLS_S3_URI, then the persisted config file.
# Deliberately fails when neither is set so the agent must ask the user.
resolve_uri() {
    if [[ -n "${HF_SKILLS_S3_URI:-}" ]]; then
        normalize_uri "$HF_SKILLS_S3_URI"
        return 0
    fi
    if [[ -f "$CONFIG_FILE" ]]; then
        local saved
        saved=$(grep -m1 '^uri=' "$CONFIG_FILE" 2>/dev/null | cut -d= -f2-)
        if [[ -n "$saved" ]]; then
            normalize_uri "$saved"
            return 0
        fi
    fi
    cat >&2 <<'EOF'
ERROR: no S3 URI configured for skill distribution.

Neither HF_SKILLS_S3_URI is set nor a URI has been saved before, so there is
nothing to sync against. Ask the user which bucket and prefix to use, then run:

    s3-skill.sh config s3://<bucket>/<prefix>

The URI is saved and reused for subsequent commands.
EOF
    exit 1
}

save_uri() {
    local uri
    uri=$(normalize_uri "$1")
    umask 077
    printf 'uri=%s\n' "$uri" > "$CONFIG_FILE"
    chmod 600 "$CONFIG_FILE" 2>/dev/null || true
    note "saved: $uri"
    note "(stored in $CONFIG_FILE; HF_SKILLS_S3_URI overrides it when set)"
}

# ---------------------------------------------------------------- manifest

is_managed()  { [[ -f "$MANIFEST" ]] && grep -qxF -- "$1" "$MANIFEST"; }
mark_managed() {
    is_managed "$1" && return 0
    umask 077
    printf '%s\n' "$1" >> "$MANIFEST"
}
is_reserved() { grep -qw -- "$1" <<< "$RESERVED"; }

# ---------------------------------------------------------------- validation

# A skill directory must contain SKILL.md, only whitelisted paths, a frontmatter
# name matching the directory name, and must stay within the size caps.
# Prints a rejection reason on stdout and returns 1 when invalid.
validate_skill_dir() {
    local dir="$1" name="$2"
    local rel count=0 total=0 size

    [[ -f "$dir/SKILL.md" ]] || { echo "no SKILL.md"; return 1; }

    local fm
    fm=$(awk -F': *' '/^name:/{print $2; exit}' "$dir/SKILL.md" | tr -d '"'"'"'\r' | xargs)
    if [[ -n "$fm" && "$fm" != "$name" ]]; then
        echo "frontmatter name '$fm' does not match directory '$name'"
        return 1
    fi

    while IFS= read -r f; do
        rel="${f#"$dir"/}"
        if [[ ! "$rel" =~ ^(SKILL\.md|scripts/[^/]+|references/[^/]+)$ ]]; then
            echo "illegal path '$rel' (allowed: SKILL.md, scripts/*, references/*)"
            return 1
        fi
        size=$(stat -c %s "$f" 2>/dev/null || echo 0)
        if [[ "$size" -gt "$MAX_FILE_BYTES" ]]; then
            echo "file '$rel' is ${size}B, exceeds ${MAX_FILE_BYTES}B cap"
            return 1
        fi
        count=$((count + 1))
        total=$((total + size))
    done < <(find "$dir" -type f 2>/dev/null)

    [[ "$count" -le "$MAX_SKILL_FILES" ]]  || { echo "$count files, exceeds $MAX_SKILL_FILES cap"; return 1; }
    [[ "$total" -le "$MAX_TOTAL_BYTES" ]]  || { echo "${total}B total, exceeds ${MAX_TOTAL_BYTES}B cap"; return 1; }
    return 0
}

# Runs the audit gate. CRITICAL findings block; HIGH is reported but allowed
# through, matching the established safe-install convention.
audit_gate() {
    local dir="$1" out crit high
    if [[ ! -x "$AUDIT" && ! -f "$AUDIT" ]]; then
        echo "audit script not found at $AUDIT — refusing to accept unaudited content"
        return 1
    fi
    out=$(bash "$AUDIT" "$dir" --json 2>/dev/null)
    crit=$(jq -r '.summary.critical // 0' <<< "$out" 2>/dev/null || echo 0)
    high=$(jq -r '.summary.high // 0' <<< "$out" 2>/dev/null || echo 0)
    if [[ "$crit" -gt 0 ]]; then
        echo "audit found $crit CRITICAL issue(s)"
        jq -r '.findings[] | select(.severity=="CRITICAL") | "      \(.file):\(.line) \(.message)"' <<< "$out" 2>/dev/null >&2
        return 1
    fi
    [[ "$high" -gt 0 ]] && note "    warning: $high HIGH finding(s) — review before use"
    return 0
}

# ---------------------------------------------------------------- commands

cmd_config() {
    if [[ $# -ge 1 ]]; then
        save_uri "$1"
        return 0
    fi
    if [[ -n "${HF_SKILLS_S3_URI:-}" ]]; then
        note "uri: $(normalize_uri "$HF_SKILLS_S3_URI")  (from HF_SKILLS_S3_URI)"
    elif [[ -f "$CONFIG_FILE" ]]; then
        note "uri: $(grep -m1 '^uri=' "$CONFIG_FILE" | cut -d= -f2-)  (from $CONFIG_FILE)"
    else
        note "no URI configured. Ask the user, then: s3-skill.sh config s3://<bucket>/<prefix>"
        return 1
    fi
    note "skills_dir: $SKILLS_ROOT"
    [[ -f "$MANIFEST" ]] && note "s3-managed skills: $(tr '\n' ' ' < "$MANIFEST")"
    return 0
}

cmd_list() {
    require_deps
    local uri; uri=$(resolve_uri) || exit 1
    note "skills in $uri:"
    aws s3 ls "$uri/" 2>/dev/null | awk '/PRE/{gsub("/","",$2); print "  " $2}' \
        || die "cannot list $uri (check the URI and your permissions)"
}

cmd_pull() {
    require_deps
    local uri; uri=$(resolve_uri) || exit 1

    # Staging lives outside skills_dir on purpose — see header notes.
    staging=$(mktemp -d "${TMPDIR:-/tmp}/hf-s3-pull.XXXXXX") || die "cannot create staging dir"
    trap 'rm -rf "${staging:-}"' EXIT

    if [[ $# -gt 0 ]]; then
        local n
        for n in "$@"; do
            aws s3 sync "$uri/$n/" "$staging/$n/" --only-show-errors 2>/dev/null \
                || note "  warning: cannot fetch '$n' from $uri"
        done
    else
        aws s3 sync "$uri/" "$staging/" --only-show-errors 2>/dev/null \
            || die "cannot sync from $uri (check the URI and your permissions)"
    fi

    # Remote content must never arrive executable, regardless of local umask.
    find "$staging" -type f -exec chmod a-x {} + 2>/dev/null || true

    local found=0 accepted=0 rejected=0
    local d name reason action
    for d in "$staging"/*/; do
        [[ -d "$d" ]] || continue
        found=$((found + 1))
        name=$(basename "$d")
        d="${d%/}"

        if is_reserved "$name"; then
            note "  REJECT $name — reserved bundled skill name (shadowing attempt)"
            rejected=$((rejected + 1)); continue
        fi
        if [[ -e "$SKILLS_ROOT/$name" ]] && ! is_managed "$name"; then
            note "  REJECT $name — a local skill with this name already exists (not S3-managed)"
            rejected=$((rejected + 1)); continue
        fi
        if ! reason=$(validate_skill_dir "$d" "$name"); then
            note "  REJECT $name — $reason"
            rejected=$((rejected + 1)); continue
        fi
        if ! reason=$(audit_gate "$d"); then
            note "  REJECT $name — $reason"
            rejected=$((rejected + 1)); continue
        fi

        action="added"
        local backup=""
        if [[ -e "$SKILLS_ROOT/$name" ]]; then
            action="updated"
            backup="$SKILLS_ROOT/.s3-bak-$$-$name"
            mv "$SKILLS_ROOT/$name" "$backup" || { note "  REJECT $name — cannot move existing dir aside"; rejected=$((rejected+1)); continue; }
        fi
        if mv "$d" "$SKILLS_ROOT/$name" 2>/dev/null; then
            [[ -n "$backup" ]] && rm -rf "$backup"
            mark_managed "$name"
            note "  ACCEPT $name ($action)"
            accepted=$((accepted + 1))
        else
            [[ -n "$backup" ]] && mv "$backup" "$SKILLS_ROOT/$name"
            note "  REJECT $name — commit failed, rolled back"
            rejected=$((rejected + 1))
        fi
    done

    [[ "$found" -eq 0 ]] && note "  no skills found at $uri"
    note ""
    note "pulled: $accepted accepted, $rejected rejected (of $found found)"
    if [[ "$accepted" -gt 0 ]]; then
        note "New skills become visible to a NEW agent session (the loader polls every 60s,"
        note "but a session's prompt is built when it starts). Existing sessions won't see them."
    fi
}

cmd_push() {
    require_deps
    [[ $# -ge 1 ]] || die "usage: s3-skill.sh push <skill-dir>"
    local src="${1%/}"
    [[ -d "$src" ]] || die "not a directory: $src"
    local name; name=$(basename "$src")
    local uri; uri=$(resolve_uri) || exit 1

    local reason
    if ! reason=$(validate_skill_dir "$src" "$name"); then
        die "refusing to publish '$name' — $reason"
    fi
    if ! reason=$(audit_gate "$src"); then
        die "refusing to publish '$name' — $reason"
    fi

    aws s3 sync "$src/" "$uri/$name/" --delete --only-show-errors 2>/dev/null \
        || die "upload failed for $uri/$name/"
    note "published: $name -> $uri/$name/"
}

cmd_upload() {
    require_deps
    [[ $# -ge 2 ]] || die "usage: s3-skill.sh upload <local-path> <s3-uri>"
    local src="$1" dst; dst=$(normalize_uri "$2")
    if [[ -d "$src" ]]; then
        # sync (not cp) so per-file content types are inferred by extension.
        aws s3 sync "${src%/}/" "$dst/" --only-show-errors 2>/dev/null \
            || die "upload failed: $src -> $dst/"
        note "uploaded directory: $src -> $dst/"
    elif [[ -f "$src" ]]; then
        aws s3 cp "$src" "$dst" --only-show-errors 2>/dev/null \
            || die "upload failed: $src -> $dst"
        note "uploaded file: $src -> $dst"
    else
        die "no such file or directory: $src"
    fi
}

cmd_presign() {
    require_deps
    [[ $# -ge 1 ]] || die "usage: s3-skill.sh presign <s3-uri> [seconds]"
    local uri; uri=$(normalize_uri "$1")
    local secs="${2:-3600}"
    [[ "$secs" =~ ^[0-9]+$ ]] || die "expiry must be a whole number of seconds: $secs"
    if [[ "$secs" -gt "$MAX_PRESIGN_SECONDS" ]]; then
        note "note: clamping expiry to $MAX_PRESIGN_SECONDS s (S3 signature maximum of 7 days)"
        secs="$MAX_PRESIGN_SECONDS"
    fi
    [[ "$secs" -gt 0 ]] || die "expiry must be greater than zero"
    aws s3 presign "$uri" --expires-in "$secs" 2>/dev/null \
        || die "presign failed for $uri"
    note "(valid for ${secs}s; anyone holding this URL can download the object)" >&2
}

usage() {
    sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    exit 1
}

main() {
    [[ $# -ge 1 ]] || usage
    local sub="$1"; shift
    case "$sub" in
        config)  cmd_config "$@" ;;
        list)    cmd_list "$@" ;;
        pull)    cmd_pull "$@" ;;
        push)    cmd_push "$@" ;;
        upload)  cmd_upload "$@" ;;
        presign) cmd_presign "$@" ;;
        -h|--help|help) usage ;;
        *) die "unknown subcommand '$sub' (try: config, list, pull, push, upload, presign)" ;;
    esac
}

main "$@"
