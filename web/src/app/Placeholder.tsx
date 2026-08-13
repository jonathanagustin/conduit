import { AccessibleNonIdealState } from "../components/index.js";

/** Stands in for a screen that has not been built yet. It names itself so a
 * route that resolves to nothing is distinguishable from one that is wired to
 * the wrong place. */
export function Placeholder({ title }: { title: string }) {
  return (
    <AccessibleNonIdealState
      headingLevel={1}
      icon="build"
      title={title}
      description="This screen is not implemented yet."
    />
  );
}
