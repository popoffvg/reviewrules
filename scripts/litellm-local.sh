#!/usr/bin/env bash
# Starts the LiteLLM proxy for reviewrules on 127.0.0.1:4000.
# The master key lives in $TMPDIR/reviewrules-litellm.key (mode 600); reviewrules reads it via LITELLM_API_KEY:
#   LITELLM_API_KEY="$(cat "$TMPDIR/reviewrules-litellm.key")" reviewrules --base origin/main
set -euo pipefail

[[ -s "${CHATGPT_TOKEN_DIR:-$HOME/.config/litellm/chatgpt}/${CHATGPT_AUTH_FILE:-auth.json}" ]] || {
  echo "reviewrules litellm: no ChatGPT login; run the device login from README first" >&2
  exit 1
}
key_file="${TMPDIR:-/tmp}/reviewrules-litellm.key"
if [[ ! -s $key_file ]]; then
  (umask 077 && echo "sk-$(openssl rand -hex 24)" > "$key_file")
fi
repo_root=$(cd "$(dirname "$0")/.." && pwd)
# LiteLLM's chatgpt provider prepends ~7.6 KB of Codex CLI agent instructions to every request.
# They tell the model it edits files in a shell, which conflicts with the read-only reviewer prompt.
export CHATGPT_DEFAULT_INSTRUCTIONS="${CHATGPT_DEFAULT_INSTRUCTIONS:-Follow the system and developer messages in the input.}"
LITELLM_MASTER_KEY=$(cat "$key_file") exec litellm --config "$repo_root/litellm.yaml" --host 127.0.0.1 --port "${LITELLM_PORT:-4000}"
