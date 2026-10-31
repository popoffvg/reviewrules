# Shared LiteLLM proxy for CI

One proxy on a host next to the CI runners holds the ChatGPT login. Every CI job calls it with the proxy key and never sees the login.

## Why the login cannot live in CI

The ChatGPT access token lasts about 10 days. LiteLLM then trades the refresh token for a new pair and writes it back to `auth.json`. Each refresh token works once. A CI job starts from a fixed copy and discards its file at the end, so the first refresh in CI breaks every later job. The proxy keeps the file on its own disk and refreshes it in one place.

## Set up the host once

1. Use a ChatGPT seat that belongs to a bot account, with device code login allowed in its security settings (a workspace admin allows it on Business).
2. Sign in on any machine with LiteLLM installed, then copy the file to the host:

   ```bash
   python -c "from litellm.llms.chatgpt.authenticator import Authenticator; Authenticator().get_access_token()"
   scp ~/.config/litellm/chatgpt/auth.json host:/srv/reviewrules/deploy/litellm/tokens/auth.json
   ```

3. On the host, make the file writable by the container user, create the key, and start:

   ```bash
   cd /srv/reviewrules/deploy/litellm
   chmod 700 tokens && chmod 600 tokens/auth.json
   echo "LITELLM_MASTER_KEY=sk-$(openssl rand -hex 32)" > .env && chmod 600 .env
   echo "REVIEWRULES_PROXY_BIND=<host address on the runners' network>" >> .env
   docker compose up -d
   curl -s http://<host>:4000/v1/models -H "Authorization: Bearer <the key>"   # lists gpt-6-luna, gpt-6.1-sol
   ```

4. Store the key as the CI secret `REVIEWRULES_PROXY_KEY` and the URL `http://<host>:4000/v1` as the CI variable `REVIEWRULES_PROXY_URL`.

## Run it in CI

```yaml
reviewrules:
  runs-on: [self-hosted]
  concurrency: reviewrules      # one review at a time shares the subscription allowance evenly
  steps:
    - uses: actions/checkout@v4
      with: { fetch-depth: 0 }
    - run: go install github.com/popoffvg/reviewrules@<pinned commit>
    - run: |
        printf '[ci]\nagent = "fantasy"\nbase_url = "%s"\nparallel = 4\n' "$REVIEWRULES_PROXY_URL" > "$RUNNER_TEMP/reviewrules.toml"
        reviewrules --config "$RUNNER_TEMP/reviewrules.toml" --base "origin/${{ github.base_ref }}" --json-out reviewrules.json
      env:
        LITELLM_API_KEY: ${{ secrets.REVIEWRULES_PROXY_KEY }}
        REVIEWRULES_PROXY_URL: ${{ vars.REVIEWRULES_PROXY_URL }}
    - uses: actions/upload-artifact@v4
      if: always()
      with: { name: reviewrules, path: reviewrules.json }
```

## Keep it running

- When the proxy logs `ChatGPT refresh token failed, re-login required`, sign in again (step 2) and restart the container.
- Run one proxy process only. Two processes with one `auth.json` both try the same refresh token, and one of them loses.
- Every review counts against the seat's Codex allowance. When the 5-hour window is used up, forks fail and the run shows warnings; `fail_on_unanswered = true` in `.reviewrules/settings.toml` turns them into a red job.
