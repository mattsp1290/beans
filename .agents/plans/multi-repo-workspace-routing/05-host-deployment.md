# Host Deployment

## Target Topology

```text
Mac / operator machine
  - local checkouts: ~/git/boxy, ~/git/shady, ~/git/birbparty/clckr
  - bn CLI
  - SSH tunnel to infra Postgres when needed

infra-admin@10.0.0.106
  - postgres
  - symphony-orchestrator
  - workspace volume
  - git credentials for repo fetch
  - optional squid egress policy

punk1290@10.0.0.248
  - Ollama on :11434
  - vLLM/Nemotron on :8000
```

## Repository Access

The orchestrator host must fetch repos from a network location. It should not
depend on the Mac's local filesystem paths.

Supported onboarding modes:

1. **Remote git URL**: preferred default.
2. **Infra mirror path**: for private or experimental repos mirrored onto
   `10.0.0.106`.
3. **Host-mounted seed path**: later option for air-gapped repos.

## Git Credentials

Install credentials on infra, not in `bn` rows:

```text
/run/secrets/git/ssh/github-default
/run/secrets/git/known_hosts
```

Runtime maps `auth_ref=ssh-key:github-default` to
`/run/secrets/git/ssh/github-default` and uses
`/run/secrets/git/known_hosts` for host verification.

Do not use one global `GIT_SSH_COMMAND` for all repos. The workspace router
must resolve `auth_ref` per git command and build the command environment for
that repo:

```bash
GIT_SSH_COMMAND='ssh -i /run/secrets/git/ssh/github-default -o UserKnownHostsFile=/run/secrets/git/known_hosts -o BatchMode=yes'
```

For HTTPS tokens, use credential helpers or an askpass script backed by
`/run/secrets`, never embed tokens in remote URLs.

## `bn` From Local Repos

Recommended local env:

```bash
ssh -N -L 15432:127.0.0.1:5432 infra-admin@10.0.0.106

export BN_DSN='postgres://symphony:symphony@127.0.0.1:15432/symphony?sslmode=disable'
export BN_PROJECT=agent-work
export BN_ACTOR=punk1290
```

Onboard:

```bash
cd ~/git/boxy
bn repo add boxy --path "$PWD" --remote git@github.com:punk1290/boxy.git --auth ssh-key:github-default
bn repo doctor boxy --from-orchestrator

cd ~/git/shady
bn repo add shady --path "$PWD" --remote git@github.com:punk1290/shady.git --auth ssh-key:github-default
bn repo doctor shady --from-orchestrator

cd ~/git/birbparty/clckr
bn repo add clckr --path "$PWD" --remote git@github.com:punk1290/clckr.git --auth ssh-key:github-default
bn repo doctor clckr --from-orchestrator
```

Create work:

```bash
cd ~/git/boxy
bn create "Fix flaky timer test" -d "Run go test ./... and close when green."
```

Because `.bn` contains `repo=boxy`, the issue is routed without `--repo`.

## Compose Changes

Add an infra override:

- Launch `symphony-orchestrator /etc/symphony/WORKFLOW.md`.
- Mount workflow.
- Mount git secrets read-only.
- Keep Postgres published to `127.0.0.1` only for SSH tunnel access.
- Use `tracker.kind: postgres`.
- Mount an infra policy file that names allowed git hosts and auth-ref secret
  mappings.

Example service additions:

```yaml
services:
  postgres:
    ports:
      - "127.0.0.1:5432:5432"

  symphony:
    command: ["symphony-orchestrator", "/etc/symphony/WORKFLOW.md"]
    environment:
      POSTGRES_DSN: postgres://symphony:symphony@postgres:5432/symphony?sslmode=disable
      BN_DSN: postgres://symphony:symphony@postgres:5432/symphony?sslmode=disable
    volumes:
      - ./WORKFLOW.infra.md:/etc/symphony/WORKFLOW.md:ro
      - ./secrets/git:/run/secrets/git:ro
      - ./repo-policy.yaml:/etc/symphony/repo-policy.yaml:ro
```

Example repo policy:

```yaml
allowed_git_hosts:
  - github.com
auth_refs:
  ssh-key:github-default:
    kind: ssh
    key_path: /run/secrets/git/ssh/github-default
    known_hosts_path: /run/secrets/git/known_hosts
```

## Model Routing

Use current Spark state as the initial default:

```yaml
agent:
  default_provider: openai
  provider_base_url: http://10.0.0.248:8000/v1
  provider_api_key: "-"
  primary_model: nemotron
```

For Ollama, use a separate workflow or keep it as the default provider for a
deployment:

```yaml
agent:
  default_provider: ollama
  base_url: http://10.0.0.248:11434
  primary_model: qwen3-coder:30b
  fallback_model: qwen2.5-coder:7b
```

Current `provider_tag_overrides` maps labels only to backend names. It does not
support per-provider endpoint maps, so one workflow cannot currently route some
issues to vLLM/OpenAI and some to a differently configured Ollama endpoint
unless the shared `agent.base_url` and model fields are valid for the Ollama
path.
