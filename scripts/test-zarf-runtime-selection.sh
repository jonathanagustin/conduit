#!/usr/bin/env bash
# A configured lint executable must run; an empty override must not skip lint.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
if ZARF_BIN= ruby "$root/scripts/verify-zarf-package.rb" > "$work/empty.log" 2>&1; then
 echo 'empty ZARF_BIN silently skipped executable lint' >&2;exit 1
fi
grep -q 'ZARF_BIN must name a Zarf executable' "$work/empty.log"
cat > "$work/zarf" <<STUB
#!/bin/sh
printf '%s\\n' "\$*" >> "$work/calls"
STUB
chmod +x "$work/zarf"
ZARF_BIN="$work/zarf" ruby "$root/scripts/verify-zarf-package.rb" > "$work/valid.log" 2>&1
[[ $(wc -l < "$work/calls") == 2 ]]
grep -qF "dev lint $root/deploy/zarf" "$work/calls"
grep -qF "dev lint $root/internal/packagingworkflow/fixtures/example-package" "$work/calls"
printf '#!/bin/sh\nexit 1\n' > "$work/zarf"
if ZARF_BIN="$work/zarf" ruby "$root/scripts/verify-zarf-package.rb" > "$work/failed.log" 2>&1; then
 echo 'failed executable lint was accepted' >&2;exit 1
fi
grep -q 'Zarf CLI lint failed' "$work/failed.log"
echo 'Zarf validator rejects empty/failed executables and invokes both lint targets'
