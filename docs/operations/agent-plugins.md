# Coding-agent plugins this repository expects

Conduit consumes two plugins from the `jonathanagustin-private` marketplace at
[`jonathanagustin/claude-code-on-the-web`](https://github.com/jonathanagustin/claude-code-on-the-web)
rather than carrying its own copies of what they know.

| Plugin | What it is for here |
| --- | --- |
| `ccw-sandbox` | Boot and diagnose a real k3s cluster inside the hosted coding-agent sandbox, and run containers at full fidelity. The delivery path needs a cluster to apply Helm releases to; this is where the knowledge of how to get one lives. |
| `crossreview` | Adversarially review a claim, file, or diff with an independent subagent — optionally a model from another lab — and verify its findings before accepting them. |

## Why a plugin instead of a copy

`scripts/bootstrap-k3s.sh` says in its own header that it is *"Derived from the
ccw-sandbox research"*. A derived copy is a fork with no merge path: the upstream
plugin keeps learning things about the sandbox — which kernel behaviours are
fixed, which are gone, which were misdiagnosed — and a copy silently stops
learning them the day it is taken.

That upstream repository's own rule is the reason this matters: *"Sandbox facts
have a shelf life. The environment swapped once (gVisor to Firecracker) and
invalidated everything."* A copy of a fact with a shelf life is a fact that will
be wrong later, without anyone being told.

## Registration and installation

`.claude/settings.json` registers the marketplace and marks both plugins
enabled, so the marketplace resolves for everyone who trusts this project:

```json
{
  "extraKnownMarketplaces": {
    "jonathanagustin-private": {
      "source": { "source": "github", "repo": "jonathanagustin/claude-code-on-the-web" }
    }
  },
  "enabledPlugins": {
    "ccw-sandbox@jonathanagustin-private": true,
    "crossreview@jonathanagustin-private": true
  }
}
```

**`enabledPlugins` in a project's settings does not install an external plugin.**
Since agent CLI 2.1.195, a plugin that only the project enables, and that comes
from an external source such as a GitHub repository, does not load until the
person installs it. So the checked-in settings remove the *configuration* step,
not the install step. Run once per machine:

```bash
claude plugin install ccw-sandbox@jonathanagustin-private
claude plugin install crossreview@jonathanagustin-private
```

or, inside a session, `/plugin install ccw-sandbox@jonathanagustin-private`.
Confirm with `/plugin`, and reload with `/reload-plugins` rather than restarting.

If the marketplace repository is private, installation uses the ordinary git
credential helpers (`gh auth setup-git`, SSH agent, or a credential store).
Background marketplace refresh disables credential helpers by default, so a
private HTTPS marketplace can fail silently on refresh; setting
`CLAUDE_CODE_PLUGIN_KEEP_MARKETPLACE_ON_FAILURE=1` keeps the last known-good
clone instead of dropping it. Do not embed a token in `.gitconfig` in a shared
checkout.

## What this does not do

Installing a plugin grants no publication, release, or deployment authority.
Those still require an explicit human instruction (assumption A5 and
`docs/operations/release-and-publication.md`), and no plugin changes that.
