// Each globe owns its observers and queued frame; stale callbacks cannot redraw.
export function observeGlobe({
  wrap,
  themeRoot,
  onResize,
  onTheme,
  platform = window,
}) {
  let active = true;
  let frame = null;
  const resize = platform.ResizeObserver
    ? new platform.ResizeObserver(() => {
        if (!active) return;
        if (frame !== null) platform.cancelAnimationFrame(frame);
        frame = platform.requestAnimationFrame(() => {
          frame = null;
          if (active) onResize();
        });
      })
    : null;
  const theme =
    themeRoot && platform.MutationObserver
      ? new platform.MutationObserver(() => {
          if (active) onTheme();
        })
      : null;
  resize?.observe(wrap);
  theme?.observe(themeRoot, {
    attributes: true,
    attributeFilter: ["data-theme"],
    subtree: true,
  });
  return () => {
    active = false;
    resize?.disconnect();
    theme?.disconnect();
    if (frame !== null) platform.cancelAnimationFrame(frame);
    frame = null;
  };
}
