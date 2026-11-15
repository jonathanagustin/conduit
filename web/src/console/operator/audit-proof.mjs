const version = "conduit.authorization-audit-chain/v1";
const encoder = new TextEncoder();
const hex = (bytes) =>
  Array.from(new Uint8Array(bytes), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
const sha = async (bytes) =>
  hex(await globalThis.crypto.subtle.digest("SHA-256", bytes));
const uint64 = (value) => {
  const bytes = new Uint8Array(8);
  new DataView(bytes.buffer).setBigUint64(0, BigInt(value), false);
  return bytes;
};
const join = (chunks) => {
  const bytes = new Uint8Array(
    chunks.reduce((size, chunk) => size + chunk.length, 0),
  );
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.length;
  }
  return bytes;
};
export async function proofDigest(entry) {
  if (!Number.isSafeInteger(entry.sequence) || entry.sequence < 1)
    throw new Error("Sequence is outside the browser's exact integer range.");
  if (typeof entry.payload !== "string" || entry.payload.length > 2_800_000)
    throw new Error(
      "Exact payload bytes are unavailable or exceed the verification limit.",
    );
  const binary = atob(entry.payload);
  if (btoa(binary) !== entry.payload)
    throw new Error("Payload is not canonical base64.");
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
  const payload = JSON.parse(
    new TextDecoder("utf-8", { fatal: true }).decode(bytes),
  );
  if (
    payload.id !== entry.id ||
    payload.resourceRid !== entry.resourceRid ||
    payload.subject !== entry.actor ||
    payload.action !== entry.action ||
    payload.decision !== entry.decision ||
    payload.createdAt !== entry.createdAt
  )
    throw new Error(
      "Displayed audit coordinates do not match the hashed payload.",
    );
  if (!["LIVE", "LEGACY_IMPORT"].includes(entry.origin))
    throw new Error("Unknown audit origin.");
  const fields = [
    entry.previousDigest,
    entry.id,
    entry.resourceRid,
    entry.origin,
  ].map((field) => {
    if (typeof field !== "string") throw new Error("Missing digest input.");
    return encoder.encode(field);
  });
  const framed = [encoder.encode(version), uint64(entry.sequence)];
  for (const field of [...fields, bytes])
    framed.push(uint64(field.length), field);
  return sha(join(framed));
}
// CONDUIT-INVENTED: reporting.AuditProjectionProof
// Displaced-by: a public or authorized sanitized Apollo audit payload and digest-framing contract with checkpoint and projection semantics.
export async function verifyProjection(entries) {
  if (!Array.isArray(entries) || entries.length === 0)
    return {
      ok: false,
      checked: 0,
      reason: "No returned payloads are available to verify.",
    };
  if (entries.length > 10000)
    return {
      ok: false,
      checked: 0,
      reason:
        "The returned projection exceeds the 10,000-entry verification limit.",
    };
  if (
    entries.reduce(
      (total, entry) =>
        total + (typeof entry?.payload === "string" ? entry.payload.length : 0),
      0,
    ) > 20_000_000
  )
    return {
      ok: false,
      checked: 0,
      reason: "Payloads exceed the 20 MB encoded verification limit.",
    };
  let previous = await sha(encoder.encode(version + ":genesis"));
  for (let i = 0; i < entries.length; i++) {
    const entry = entries[i];
    try {
      if (entry.sequence !== i + 1 || entry.previousDigest !== previous)
        throw new Error("Projection sequence or predecessor is discontinuous.");
      if (!/^[0-9a-f]{64}$/.test(entry.digest))
        throw new Error("Digest is not lowercase SHA-256.");
      if ((await proofDigest(entry)) !== entry.digest)
        throw new Error("Returned payload digest does not match.");
      previous = entry.digest;
    } catch (error) {
      return {
        ok: false,
        checked: i,
        reason: `Entry ${i + 1}: ${error.message}`,
      };
    }
  }
  return {
    ok: true,
    checked: entries.length,
    reason:
      "Returned payload digests and links are internally consistent. This is a reconstructed projection; no independent checkpoint, stored commit order or event truth was verified.",
  };
}
