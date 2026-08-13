import { Button, Spinner } from "@blueprintjs/core";
import type { ReactNode } from "react";

import { ApiError } from "../api/errors.js";
import { AccessibleNonIdealState } from "./AccessibleNonIdealState.js";

/** One place where loading and failure become a screen.
 *
 * Every list and detail view has the same three outcomes, and they have to read
 * the same way each time: an operator should not have to learn per screen what
 * "nothing here" looks like. In particular 401 is separated from every other
 * failure, because "sign in" and "this could not be read" are different
 * instructions. */
export interface QueryStateProps<T> {
  isPending: boolean;
  error: Error | null;
  data: T | undefined;
  /** What this screen is reading, named for the failure message. */
  subject: string;
  /** Match the state heading to the surrounding page outline. */
  headingLevel?: 1 | 2;
  onRetry: () => void;
  children: (data: T) => ReactNode;
}

export function QueryState<T>({
  isPending,
  error,
  data,
  subject,
  headingLevel = 1,
  onRetry,
  children,
}: QueryStateProps<T>) {
  if (error !== null) {
    const api = error instanceof ApiError ? error : undefined;
    if (api?.unauthenticated === true) {
      return (
        <AccessibleNonIdealState
          headingLevel={headingLevel}
          icon="lock"
          title="Not signed in"
          description="This console is served behind an operator identity. The Hub did not recognise this session."
        />
      );
    }
    if (api?.forbidden === true) {
      return (
        <AccessibleNonIdealState
          headingLevel={headingLevel}
          icon="eye-off"
          title="Not visible to you"
          description={`Your grants do not cover ${subject}.`}
        />
      );
    }
    return (
      <AccessibleNonIdealState
        headingLevel={headingLevel}
        icon="error"
        title={`${subject} could not be read`}
        description={error.message}
        action={<Button text="Try again" onClick={onRetry} />}
      />
    );
  }
  if (isPending || data === undefined) {
    return (
      <AccessibleNonIdealState
        headingLevel={headingLevel}
        icon={<Spinner />}
        title={`Reading ${subject}`}
      />
    );
  }
  return <>{children(data)}</>;
}
