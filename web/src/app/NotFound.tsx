import { Link, useLocation } from "react-router-dom";

import { AccessibleNonIdealState } from "../components/index.js";

export function NotFound() {
  const location = useLocation();
  return (
    <AccessibleNonIdealState
      headingLevel={1}
      icon="path-search"
      title="No such page"
      description={`Nothing is routed at ${location.pathname}.`}
      action={<Link to="/fleet">Go to the fleet</Link>}
    />
  );
}
