import { Assistant } from "./assistant";

export default function Home() {
  return <Assistant demo={process.env.CONDUIT_AGENT_MODE === "demo"} />;
}

export const dynamic = "force-dynamic";
