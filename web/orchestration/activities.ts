import type { Evidence, Proposal } from "./contracts";

// Rehearsal adapters deliberately have no external side effects. Real Job and
// Hub adapters must preserve these idempotency coordinates and authorization.
export async function validateProposal(input: {
  stepId: string;
  proposal: Proposal;
}): Promise<Evidence> {
  const passed = !input.proposal.failValidation;
  return {
    simulation: true,
    passed,
    checks: [
      { name: "build fixture", passed: true },
      { name: "test fixture", passed: true },
      { name: "scan fixture", passed },
    ],
  };
}

export async function simulateDeployment(input: {
  stepId: string;
  approved: boolean;
  evidence: Evidence;
}) {
  if (!input.approved || !input.evidence.passed || !input.evidence.simulation) {
    throw new Error(
      "A successful rehearsal requires explicit approval and passing fixture evidence",
    );
  }
  return {
    id: input.stepId,
    simulation: true as const,
    deployed: false as const,
  };
}
