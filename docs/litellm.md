# LiteLLM on a ChatGPT subscription

Run the setup from a clone of this repository. The local script uses the clone's `litellm.yaml`. Select this backend in your user config:

```toml
[local]
agent = "fantasy"
base_url = "http://localhost:4000/v1"
```

The models run on a ChatGPT plan that includes Codex. Allow device code login in ChatGPT's security settings, then sign in once:

```bash
uv tool install 'litellm[proxy]==1.106.6'
~/.local/share/uv/tools/litellm/bin/python -c \
  "from litellm.llms.chatgpt.authenticator import Authenticator; Authenticator().get_access_token()"
chmod 600 ~/.config/litellm/chatgpt/auth.json
scripts/litellm-local.sh                 # keep running; 127.0.0.1:4000
```

In a second terminal, change to the repository you want to review and run:

```bash
LITELLM_API_KEY="$(cat "${TMPDIR:-/tmp}/reviewrules-litellm.key")" reviewrules --base origin/main --worktree
```

The tool sends one `litellm_session_id` per review so the ChatGPT backend can share the prompt cache across forks. For a shared proxy in CI see [the deployment guide](../deploy/litellm/README.md).

This route consumes your ChatGPT plan's Codex allowance. See [backend choices](backends.md) for local CLI and direct API alternatives.
