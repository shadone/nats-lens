import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { ExpiringKind, GetAccountsAndAuthResponseSchema } from "../gen/natslens/v1/accounts_pb";
import { accountsAndAuth } from "./accounts";

const AT = Date.parse("2026-10-17T14:30:00Z");

describe("accountsAndAuth", () => {
  it("reads an empty response as no collection", () => {
    const d = accountsAndAuth(create(GetAccountsAndAuthResponseSchema, {}));
    expect(d.collected).toBe(false);
    expect(d.epochAt).toBeUndefined();
    expect(d.accounts).toEqual([]);
  });

  it("keeps unset values undefined and set ones, 0 and -1 among them, as numbers", () => {
    const d = accountsAndAuth(
      create(GetAccountsAndAuthResponseSchema, {
        epoch: 7n,
        epochAt: timestampFromMs(AT),
        operatorMode: true,
        accounts: [
          { account: "APP", connections: 0, subscriptions: 12n, connLimit: -1n, diskLimit: 0n, detailsKnown: true, hasJwt: true },
          { account: "SYS", isSystem: true },
        ],
        accountsTotal: 2,
        failedLogins: [{ user: "", host: "203.0.113.12", reason: "Authentication Timeout", count: 96n, last: timestampFromMs(AT - 1000) }],
        failedLoginsTotal: 96n,
        expiring: [
          { kind: ExpiringKind.SERVER_CERTIFICATE, server: "nats-eu-0", listener: "client", expires: timestampFromMs(AT + 1000) },
          { kind: ExpiringKind.ACCOUNT_JWT, account: "APP", expired: true },
          { kind: ExpiringKind.USER_JWT, account: "APP", user: "pos-terminal", connections: 3 },
        ],
      }),
    );
    expect(d.collected).toBe(true);
    expect(d.epochAt).toBe(AT);
    const [app, sys] = d.accounts;
    expect(app).toMatchObject({ connections: 0, subscriptions: 12, connLimit: -1, diskLimit: 0, detailsKnown: true, hasJwt: true });
    expect(app.jsStorage).toBeUndefined();
    expect(app.jwtExpires).toBeUndefined();
    expect(sys).toMatchObject({ isSystem: true, connections: undefined, subscriptions: undefined, connLimit: undefined, diskLimit: undefined, imports: undefined });
    expect(d.failedLogins[0]).toEqual({ user: "", host: "203.0.113.12", reason: "Authentication Timeout", account: "", accountName: "", count: 96, last: AT - 1000 });
    expect(d.failedLoginsTotal).toBe(96);
    expect(d.expiring.map((e) => e.kind)).toEqual(["certificate", "accountJwt", "userJwt"]);
    expect(d.expiring[1].expires).toBeUndefined();
    expect(d.expiring[2].connections).toBe(3);
  });
});
