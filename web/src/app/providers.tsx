import type { ReactNode } from "react";
import { BrowserRouter } from "react-router-dom";

// The Hub mounts this console at /console/. The Vite base and the Hub path
// check use the same prefix.
export function Providers({ children }: { children: ReactNode }) {
  return <BrowserRouter basename="/console/">{children}</BrowserRouter>;
}
