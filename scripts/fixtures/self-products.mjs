// Synthetic metadata and bytes only. These are not released artifacts or binaries.
import { createHash } from "node:crypto";
export function selfProductFixture() {
  const repository = "jonathanagustin/conduit", version = "0.17.5", commit = "1".repeat(40);
  const oci = { apiVersion: "platform.delivery/oci-release/v2", version, commit, images: {} };
  for (const [component, digit] of [["hub", "2"], ["spoke-agent", "3"]]) {
    const image = `ghcr.io/${repository}`, digest = `sha256:${digit.repeat(64)}`;
    const receipts = extension => ["amd64", "arm64"].map(architecture => ({architecture, file:`${component}-${architecture}.${extension}`,sha256:"4".repeat(64)}));
    oci.images[component] = { image, digest, reference:`${image}@${digest}`, sboms:receipts("spdx.json"), vulnerabilityReports:receipts("grype.json") };
  }
  const helm = { apiVersion: "platform.delivery/helm-release/v1", version, commit, charts:["hub", "spoke"].map((component,index)=>{
    const digest=`sha256:${String(index+5).repeat(64)}`;
    return {component,version,commit,digest,reference:`ghcr.io/${repository}/platform-${component}:${version}@${digest}`};
  }) };
  const archives = new Map(), provenance = new Map(), checksums = [];
  for (const os of ["darwin", "linux", "windows"]) for (const architecture of ["amd64", "arm64"]) {
    const target=`${os}/${architecture}`,stem=`conduit_${version}_${os}_${architecture}`,file=`${stem}.${os==="windows"?"zip":"tar.gz"}`;
    const bytes=Buffer.from(`SYNTHETIC NON-EXECUTABLE FIXTURE ${target}\n`);
    archives.set(file,bytes);checksums.push(`${createHash("sha256").update(bytes).digest("hex")}  ${file}`);
    provenance.set(`${stem}.slsa.json`,{buildDefinition:{buildType:"https://platform.delivery/buildtypes/conduit-cli/v1",externalParameters:{target,version},resolvedDependencies:[{uri:`git+https://github.com/${repository}@refs/tags/v${version}`,digest:{gitCommit:commit}}]},runDetails:{builder:{id:`https://github.com/${repository}/.github/workflows/platform-images.yml@refs/tags/v${version}`}}});
  }
  return { repository, version, oci, helm, checksums:checksums.join("\n")+"\n", archives, provenance,
    readCLI:file=>archives.get(file),readProvenance:file=>provenance.get(file) };
}
