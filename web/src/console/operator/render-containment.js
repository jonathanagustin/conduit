/*
 * Containment for one workspace view. A renderer that throws must not leave
 * the workspace blank or half painted: the view is replaced by a stated
 * failure, and the shell and navigation around it keep working.
 */

/** Every interpolated value passes through `esc`; nothing here is raw. */
export function failureMarkup(screen, error, esc) {
  const name = error instanceof Error ? error.name || "Error" : "Error";
  const message =
    error instanceof Error ? String(error.message).slice(0, 300) : "";
  return `<section class="so-panel p-3 space-y-2" role="alert" data-render-failure="${esc(screen)}">
    <h2 class="text-[13px] font-semibold">This view failed to render</h2>
    <p class="text-[12px]">View <span class="code-id" data-render-failure-screen>${esc(screen)}</span> stopped with an error. Nothing shown here is a Hub record. The navigation and the other views are unaffected.</p>
    <p class="text-[12px] code-id" data-render-failure-error>${esc(name)}${message ? `: ${esc(message)}` : ""}</p>
    <button type="button" class="so-btn" data-render-reload>Reload</button>
  </section>`;
}

/**
 * Run `paint` for `screen` into `root`. Returns true when it painted, false
 * when it threw and the contained failure state was painted instead.
 */
export function renderContained(
  root,
  screen,
  paint,
  { esc, log = () => {}, reload = () => {} },
) {
  try {
    paint();
    return true;
  } catch (error) {
    try {
      log(screen, error);
    } catch {
      // A failing log sink must not undo the containment.
    }
    // Handlers the failed or previous view left on the node would act on
    // markup that is no longer there.
    root.onclick = null;
    root.onchange = null;
    root.innerHTML = failureMarkup(screen, error, esc);
    root
      .querySelector("[data-render-reload]")
      ?.addEventListener("click", () => reload());
    return false;
  }
}
