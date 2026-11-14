# Third-party assets in the console

| Asset | Source | License |
| --- | --- | --- |
| `web/src/console/operator/assets/blue-marble-day-2048.webp` (light-theme fleet globe) | NASA Earth Observatory, Blue Marble Next Generation with topography and bathymetry, April 2004 (`world.topo.bathy.200404.3x5400x2700.jpg`), downscaled to 2048×1024 WebP | Public domain (NASA imagery). Credit: NASA Earth Observatory / Reto Stöckli. |

The image is bundled with the console and served from the Hub's own origin. The
Hub's content security policy (`img-src 'self' data:`) does not allow loading it
from anywhere else. The dark theme keeps the drawn night globe and loads no
image.
