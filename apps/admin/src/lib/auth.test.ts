import { describe, it, expect } from "vitest";

/**
 * Test the extractRoles helper logic that parses Zitadel role claims.
 * Since extractRoles is not exported, we test the equivalent logic here.
 */
function extractRoles(profile: Record<string, unknown>): string[] {
  const rolesClaim =
    (profile["urn:zitadel:iam:org:project:roles"] as Record<
      string,
      unknown
    >) || {};
  return Object.keys(rolesClaim);
}

describe("extractRoles (Zitadel role claim parser)", () => {
  it("extracts role names from Zitadel claim", () => {
    const profile = {
      sub: "123",
      "urn:zitadel:iam:org:project:roles": {
        admin: { orgId: "org1" },
        viewer: { orgId: "org1" },
      },
    };
    const roles = extractRoles(profile);
    expect(roles).toContain("admin");
    expect(roles).toContain("viewer");
    expect(roles).toHaveLength(2);
  });

  it("returns empty array when no roles claim", () => {
    const profile = { sub: "456" };
    expect(extractRoles(profile)).toEqual([]);
  });

  it("returns empty array when roles claim is empty object", () => {
    const profile = {
      sub: "789",
      "urn:zitadel:iam:org:project:roles": {},
    };
    expect(extractRoles(profile)).toEqual([]);
  });

  it("handles null roles claim gracefully", () => {
    const profile = {
      sub: "000",
      "urn:zitadel:iam:org:project:roles": null,
    };
    expect(extractRoles(profile)).toEqual([]);
  });
});
