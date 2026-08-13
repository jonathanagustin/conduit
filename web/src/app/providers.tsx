import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router-dom";

// Server state lives in the query cache, route state lives in the URL, and form
// state stays local. There is deliberately no general client-side store until a
// workflow proves one is needed.
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // The console polls; it does not hold a socket open. Refetching on focus
      // is what makes a bounded poll feel live without shortening the interval.
      refetchOnWindowFocus: true,
      staleTime: 10_000,
      retry: 1,
    },
  },
});

export function Providers({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter basename="/console">{children}</BrowserRouter>
    </QueryClientProvider>
  );
}
