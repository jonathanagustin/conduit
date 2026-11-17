# Console browser verification

The normal console suite has Chromium, Firefox and WebKit projects using the
revisions selected by the frozen Playwright dependency. Functional and axe
accessibility specs run on all three engines. Chromium alone runs
`visual-areas.spec.ts`: its pixel baselines are explicitly qualified for native
Linux and the named QEMU renderer, not other rendering engines.

`platform-ci.yml` installs all three browsers and their OS dependencies through
Playwright's standard `--with-deps` installer, with the existing bounded retry
policy. `test-console.sh` installs the same browser revisions before executing
the complete suite. Browser test failures do not receive retries. The named
local Chromium launch profile remains unchanged; Firefox/WebKit receive no
Chromium-specific flags.

For a focused local engine run after installing its dependencies:

```sh
cd web
pnpm exec playwright install firefox webkit
pnpm exec playwright test --project=firefox
pnpm exec playwright test --project=webkit
```

A downloaded binary is not a qualified browser. Missing libraries or startup
failures are infrastructure failures, not passing or skipped assertions. A
focused project result is not a complete local-CI receipt. Review the complete
native hosted gate for the proposed head before merging browser changes.

## Pointer and focus stability

The compact header reserves space for identity and live-status controls before
their first reads complete. A held pointer must still target the same button
after those reads; font rounding alone is not a lost click. Unchanged shell
markup and area tabs retain their DOM targets. The remaining area content still
repaints from the new read model. If navigation markup changes, a still-rendered
control that had focus is restored by its action identity. Area tabs and a
completed delivery outcome likewise retain focus through workspace repaint.
No control is activated by restoring focus, and removed controls are not revived.

The header regression holds the session response between pointer down and up,
checks the actual hit target, and requires the assistant to open. The keyboard
journey uses real forward/backward Tab and lets each key's focus/layout update
settle before inspecting the next target;
it does not depend on Tab wrapping through browser chrome. Proposal and
independent approval still require both confirmations and exact write counts.
A deliberate completed-outcome repaint must retain focus before navigation.

A closed mobile navigation drawer has no shadow over the workbench. Its shadow
returns only while open, and a read refresh preserves the operator's explicit
open/close state. Mobile baseline changes for this behavior require independent
review of native and QEMU captures; pixel tolerance remains zero.

The mobile shadow repair's native captures come from source
`1d7828c5cfde151ae0355045c5b2897e8775eea2`, hosted run `44174067804`, artifact
`14368928276` (ZIP SHA-256
`28eb471e66e5adc366738df04d84a01d3bed11c5987f7702c36b4cf4540431a1`).
All 14 full-size native phone images were reviewed independently of QEMU;
changed pixels are confined to the left 60 pixels of the 390-by-844 viewport.
That capture run passed 365 cases and failed the 14 old phone expectations;
it is capture provenance, not a passing qualification of the new baselines.
