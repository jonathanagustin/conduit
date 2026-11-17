import { Component, type ErrorInfo, type ReactNode } from "react";

import { logClientError } from "../runtime/client-errors";

interface Props {
  /** Names the contained region in the fallback's test id. */
  region: "console" | "assistant";
  children: ReactNode;
}

interface State {
  failed: boolean;
}

/**
 * Contains a throw from the React-mounted parts of the console. Without it
 * React unmounts the whole root and the operator is left with a blank page.
 *
 * The fallback states the failure and offers a reload. It renders no error
 * text: the record goes to the console, once, through the shared sink.
 */
export class ConsoleErrorBoundary extends Component<Props, State> {
  override state: State = { failed: false };

  static getDerivedStateFromError(): State {
    return { failed: true };
  }

  override componentDidCatch(error: Error, _info: ErrorInfo): void {
    logClientError("react", error);
  }

  override render(): ReactNode {
    if (!this.state.failed) return this.props.children;
    const whole = this.props.region === "console";
    return (
      <div
        role="alert"
        data-testid={`${this.props.region}-render-failed`}
        style={{ padding: 12 }}
      >
        <p>
          {whole
            ? "The console failed to render."
            : "The assistant failed to render. The workbench is unaffected."}
        </p>
        <button type="button" onClick={() => window.location.reload()}>
          Reload
        </button>
      </div>
    );
  }
}
