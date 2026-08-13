# Vendored Zenith Protocol

This directory is a repository-local copy of the private
`drengskapur/plugin-zenith-protocol` repository at commit
`93fdc841e9996ac7b338607fc461cbc4bb9ada5d`.

The copy includes the full playbook, commands, hooks, skills, scripts, and
templates so Conduit's operating model can be read and verified without a
separate plugin checkout. Conduit's ledgers remain under `docs/operations/` as
configured by the repository-root `.zenith-protocol.json`.

The upstream package declares `"private": true` and `"license": "UNLICENSED"`.
This vendored copy is proprietary material covered by Conduit's repository
license. Do not publish it to a public package or plugin registry.

To refresh it, copy from an exact reviewed upstream commit, update the commit
above, run the vendored test suite, run its conformance checker against
Conduit, and review the diff before merge. Do not copy `.git` metadata or local
credentials.
