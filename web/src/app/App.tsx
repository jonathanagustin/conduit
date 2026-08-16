import { useEffect } from "react";
import { Route, Routes } from "react-router-dom";
import { Callout } from "@blueprintjs/core";

import { useSession } from "../api/session.js";
import { AppFrame, NavigationRail } from "../components/index.js";
import { FleetPage } from "../features/fleet/FleetPage.js";
import { PlansPage } from "../features/plans/PlansPage.js";
import { PlanDetailPage } from "../features/plans/PlanDetailPage.js";
import { ChangeRequestsPage } from "../features/changes/ChangeRequestsPage.js";
import { ChangeRequestDetailPage } from "../features/changes/ChangeRequestDetailPage.js";
import { EnvironmentsPage } from "../features/environments/EnvironmentsPage.js";
import { EnvironmentDetailPage } from "../features/environments/EnvironmentDetailPage.js";
import { ProductsPage, ProductDetailPage } from "../features/products/ProductsPage.js";
import { PromotionsPage } from "../features/promotions/PromotionsPage.js";
import { RiskPage } from "../features/risk/RiskPage.js";
import { TeamsPage, TeamDetailPage } from "../features/teams/TeamsPage.js";
import { ExportPage } from "../features/exports/ExportPage.js";
import { OverviewPage } from "../features/overview/OverviewPage.js";
import { APPLICATION_DESTINATIONS, RAIL_DESTINATIONS } from "./navigation.js";
import { NotFound } from "./NotFound.js";
import { Placeholder } from "./Placeholder.js";
import "./app.css";
import type { ConsoleBootstrap } from "../runtime/bootstrap.js";

export interface AppProps {
  bootstrap: ConsoleBootstrap;
}

export function App({ bootstrap }: AppProps) {
	useEffect(() => {
		document.title = `${bootstrap.brand.title} - ${bootstrap.tenant.displayName}`;
	}, [bootstrap.brand.title, bootstrap.tenant.displayName]);
	const session = useSession();
  const enabledApplications = new Set(
    (session.data?.applications ?? [])
      .filter((application) => application.enabled)
      .map((application) => application.id),
  );
  const applications = APPLICATION_DESTINATIONS.filter((destination) =>
    enabledApplications.has(destination.id),
  );
  return (
		<AppFrame
			icon={bootstrap.brand.icon}
			accent={bootstrap.brand.accent}
			title={bootstrap.brand.title}
			subtitle={bootstrap.brand.subtitle}
		>
      <div className="console-shell">
        <NavigationRail primary={RAIL_DESTINATIONS} applications={applications} />
        <div className="console-shell__canvas" data-testid="console-workspace-canvas">
          {session.error !== null ? (
            <Callout
              compact
              intent="warning"
              icon="warning-sign"
              role="status"
              data-testid="session-capabilities-unavailable"
            >
              Session capabilities could not be read, so application navigation is incomplete.
            </Callout>
          ) : null}
          <Routes>
            <Route path="/" element={<OverviewPage />} />
            <Route path="/overview" element={<OverviewPage />} />
            <Route path="/fleet" element={<FleetPage />} />
            <Route path="/environments" element={<EnvironmentsPage />} />
            <Route path="/environments/:environmentId" element={<EnvironmentDetailPage />} />
            <Route path="/products" element={<ProductsPage />} />
            <Route path="/products/:product" element={<ProductDetailPage />} />
            <Route path="/promotions" element={<PromotionsPage />} />
            <Route path="/plans" element={<PlansPage />} />
            <Route path="/plans/:planId" element={<PlanDetailPage />} />
            <Route path="/change-requests" element={<ChangeRequestsPage />} />
            <Route path="/change-requests/:changeRequestId" element={<ChangeRequestDetailPage />} />
            <Route path="/export" element={<ExportPage />} />
            <Route path="/import" element={<Placeholder title="Import" />} />
            <Route path="/risk" element={<RiskPage />} />
            <Route path="/teams" element={<TeamsPage />} />
            <Route path="/teams/:teamName" element={<TeamDetailPage />} />
            <Route path="*" element={<NotFound />} />
          </Routes>
        </div>
      </div>
    </AppFrame>
  );
}
