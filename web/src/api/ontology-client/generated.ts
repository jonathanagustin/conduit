// Written by scripts/generate-ontology-client.mjs from testdata/ontology/full-metadata.json. Do not edit.

export const ONTOLOGY = "conduit" as const;

export interface Artifact {
  __apiName: "Artifact";
  __primaryKey: string;
  __title?: string;
  /** Digest */
  digest?: string | null;
  /** Identifier */
  id?: string | null;
  /** Product */
  product?: string | null;
  /** Repository */
  repository?: string | null;
  /** Tag */
  tag?: string | null;
  /** Version */
  version?: string | null;
}

export interface ChangeRequest {
  __apiName: "ChangeRequest";
  __primaryKey: string;
  __title?: string;
  /** Author */
  author?: string | null;
  /** Environment */
  environment?: string | null;
  /** Identifier */
  id?: string | null;
  /** Plan */
  planId?: string | null;
  /** Status */
  status?: string | null;
  /** Title */
  title?: string | null;
}

export interface Environment {
  __apiName: "Environment";
  __primaryKey: string;
  __title?: string;
  /** Connection */
  connection?: string | null;
  /** Generation */
  generation?: string | null;
  /** Identifier */
  id?: string | null;
  /** Default release channel */
  releaseChannel?: string | null;
}

export interface Plan {
  __apiName: "Plan";
  __primaryKey: string;
  __title?: string;
  /** Change request */
  changeRequestId?: string | null;
  /** Environment */
  environment?: string | null;
  /** Generation */
  generation?: string | null;
  /** Identifier */
  id?: string | null;
  /** Status */
  status?: string | null;
}

export interface ReleaseChannel {
  __apiName: "ReleaseChannel";
  __primaryKey: string;
  __title?: string;
  /** Archived */
  archived?: boolean | null;
  /** Description */
  description?: string | null;
  /** Environment */
  environment?: string | null;
  /** Identifier */
  id?: string | null;
  /** Name */
  name?: string | null;
}

export interface Spoke {
  __apiName: "Spoke";
  __primaryKey: string;
  __title?: string;
  /** Environment */
  environment?: string | null;
  /** Identifier */
  id?: string | null;
}

export interface WorkProfile {
  __apiName: "WorkProfile";
  __primaryKey: string;
  __title?: string;
  /** Accreditation */
  accreditation?: string | null;
  /** Description */
  description?: string | null;
  /** Display name */
  displayName?: string | null;
  /** Environment */
  environmentId?: string | null;
  /** Owner team */
  ownerTeam?: string | null;
}

export interface ObjectTypes {
  Artifact: Artifact;
  ChangeRequest: ChangeRequest;
  Environment: Environment;
  Plan: Plan;
  ReleaseChannel: ReleaseChannel;
  Spoke: Spoke;
  WorkProfile: WorkProfile;
}

export type ObjectTypeName = keyof ObjectTypes;

export const OBJECT_TYPES = {
  Artifact: {
    apiName: "Artifact",
    displayName: "Artifact",
    pluralDisplayName: "Artifacts",
    primaryKey: "id",
    titleProperty: "repository",
    properties: ["digest", "id", "product", "repository", "tag", "version"] as const,
    links: {
      plans: { target: "Plan", cardinality: "MANY" },
    },
  },
  ChangeRequest: {
    apiName: "ChangeRequest",
    displayName: "Change request",
    pluralDisplayName: "Change requests",
    primaryKey: "id",
    titleProperty: "title",
    properties: ["author", "environment", "id", "planId", "status", "title"] as const,
    links: {
      environment: { target: "Environment", cardinality: "ONE" },
      plan: { target: "Plan", cardinality: "ONE" },
    },
  },
  Environment: {
    apiName: "Environment",
    displayName: "Environment",
    pluralDisplayName: "Environments",
    primaryKey: "id",
    titleProperty: "id",
    properties: ["connection", "generation", "id", "releaseChannel"] as const,
    links: {
      changeRequests: { target: "ChangeRequest", cardinality: "MANY" },
      plans: { target: "Plan", cardinality: "MANY" },
      releaseChannels: { target: "ReleaseChannel", cardinality: "MANY" },
      reportingSpoke: { target: "Spoke", cardinality: "MANY" },
      workProfile: { target: "WorkProfile", cardinality: "ONE" },
    },
  },
  Plan: {
    apiName: "Plan",
    displayName: "Plan",
    pluralDisplayName: "Plans",
    primaryKey: "id",
    titleProperty: "id",
    properties: ["changeRequestId", "environment", "generation", "id", "status"] as const,
    links: {
      artifacts: { target: "Artifact", cardinality: "MANY" },
      changeRequest: { target: "ChangeRequest", cardinality: "ONE" },
      environment: { target: "Environment", cardinality: "ONE" },
    },
  },
  ReleaseChannel: {
    apiName: "ReleaseChannel",
    displayName: "Release channel",
    pluralDisplayName: "Release channels",
    primaryKey: "id",
    titleProperty: "name",
    properties: ["archived", "description", "environment", "id", "name"] as const,
    links: {
      environment: { target: "Environment", cardinality: "ONE" },
    },
  },
  Spoke: {
    apiName: "Spoke",
    displayName: "Spoke",
    pluralDisplayName: "Spokes",
    primaryKey: "id",
    titleProperty: "id",
    properties: ["environment", "id"] as const,
    links: {
      environments: { target: "Environment", cardinality: "MANY" },
    },
  },
  WorkProfile: {
    apiName: "WorkProfile",
    displayName: "Work profile",
    pluralDisplayName: "Work profiles",
    primaryKey: "environmentId",
    titleProperty: "displayName",
    properties: ["accreditation", "description", "displayName", "environmentId", "ownerTeam"] as const,
    links: {
      environment: { target: "Environment", cardinality: "ONE" },
    },
  },
} as const;

export interface LinkTargets {
  Artifact: {
    plans: "Plan";
  };
  ChangeRequest: {
    environment: "Environment";
    plan: "Plan";
  };
  Environment: {
    changeRequests: "ChangeRequest";
    plans: "Plan";
    releaseChannels: "ReleaseChannel";
    reportingSpoke: "Spoke";
    workProfile: "WorkProfile";
  };
  Plan: {
    artifacts: "Artifact";
    changeRequest: "ChangeRequest";
    environment: "Environment";
  };
  ReleaseChannel: {
    environment: "Environment";
  };
  Spoke: {
    environments: "Environment";
  };
  WorkProfile: {
    environment: "Environment";
  };
}

export interface CreateChangeRequestParams {
  changeId?: string;
  changeType: string;
  description?: string;
  environment: string;
  installationId?: string;
  productId?: string;
  targetId: string;
  targetKind: string;
  title?: string;
}

export interface ActionParams {
  createChangeRequest: CreateChangeRequestParams;
}

export type ActionName = keyof ActionParams;

export const ACTION_TYPES = {
  createChangeRequest: {
    apiName: "createChangeRequest",
    displayName: "Create change request",
    required: ["changeType", "environment", "targetId", "targetKind"] as const,
  },
} as const;
