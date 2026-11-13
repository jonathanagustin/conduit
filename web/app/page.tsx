import { Assistant } from "./assistant";

export default function Home() {
  return (
    <Assistant
      demo={["demo", "temporal-demo"].includes(
        process.env.CONDUIT_AGENT_MODE ?? "",
      )}
    />
  );
}

export const dynamic = "force-dynamic";
