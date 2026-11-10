import { describe, expect, it } from "vitest";
import { navFor } from "./nav";

describe("navFor and the accepted findings list", () => {
  it("keeps the account scope on the review list, and leaves it behind on a finding's page", () => {
    expect(navFor("/findings/accepted")).toMatchObject({ path: "/findings", accountScope: true });
    expect(navFor("/findings/stream-005%3AAPP%2Forders")).toMatchObject({ path: "/findings", accountScope: false });
  });
});
