# Security technology references

- Prefer standards and graduated or vendor-supported projects over custom
  cryptography, identity issuance, vulnerability databases, or secret stores.
- Pin reviewed versions and upstream primary sources. Distinguish a stable core
  feature from alpha/optional integrations in the same project.
- Platform owns authorization decisions and provider-neutral evidence; cluster
  bootstrap repositories own operator installation, credentials, trust roots,
  KMS policy, and provider configuration.
- Never copy credentials, private keys, vulnerability feeds, or entire upstream
  documentation bodies into this directory.
