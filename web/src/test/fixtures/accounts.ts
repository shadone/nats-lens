// Accounts and auth view models for tests: example-global in operator mode
// as the demo has it (APP with limits, the system account, SUPPLIERS idle
// with a key), example-prod in config mode, and a range without a
// collection.
import type { AccountRow, AccountsAndAuth, Expiring, FailedLogin } from "../../api/accounts";

export const EPOCH_AT = Date.parse("2026-10-17T14:30:00Z");
const DAY = 86_400_000;
const GIB = 1024 ** 3;

export const SUPPLIERS = "ADSUPPLIERSFAKEACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5NH";

export function account(id: string, o: Partial<AccountRow> = {}): AccountRow {
  return {
    account: id,
    accountName: id,
    isSystem: false,
    connections: 0,
    leafnodes: 0,
    subscriptions: 0,
    jsStorage: 0,
    jsMemory: 0,
    streams: 0,
    imports: 0,
    exports: 0,
    detailsKnown: true,
    hasJwt: false,
    expired: false,
    detailsAt: EPOCH_AT - 60_000,
    ...o,
  };
}

export function login(user: string, host: string, reason: string, count: number, o: Partial<FailedLogin> = {}): FailedLogin {
  return { user, host, reason, account: "", accountName: "", count, last: EPOCH_AT - 41 * 60_000, ...o };
}

export function expiring(o: Partial<Expiring>): Expiring {
  return { kind: "certificate", server: "", listener: "", account: "", accountName: "", user: "", expired: false, connections: 0, ...o };
}

export function global(): AccountsAndAuth {
  return {
    collected: true,
    epochAt: EPOCH_AT,
    operatorMode: true,
    accounts: [
      account("APP", { connections: 412, subscriptions: 3_120, jsStorage: 410 * GIB, streams: 23, connLimit: 2000, diskLimit: 500 * GIB, imports: 1, hasJwt: true }),
      account(SUPPLIERS, { accountName: "SUPPLIERS", exports: 1, connLimit: 50, diskLimit: 0, jsStorage: undefined, hasJwt: true, jwtExpires: EPOCH_AT + 12 * DAY }),
      account("SYS", { isSystem: true, subscriptions: 412, exports: 11, connLimit: -1, diskLimit: 0, jsStorage: undefined, hasJwt: true }),
    ],
    accountsTotal: 3,
    failedLogins: [
      login("label-printer", "10.20.4.31", "User Authentication Revoked", 1_440, { account: "APP", accountName: "APP", last: EPOCH_AT - 30_000 }),
      login("", "203.0.113.12", "Authentication Timeout", 96),
    ],
    failedLoginGroupsTotal: 2,
    failedLoginsTotal: 1_536,
    failedLoginsWindowStart: EPOCH_AT - DAY,
    captureGaps: false,
    expiring: [
      expiring({ kind: "userJwt", account: "APP", accountName: "APP", user: "pos-terminal", expires: EPOCH_AT + 5 * DAY, connections: 3 }),
      expiring({ kind: "accountJwt", account: SUPPLIERS, accountName: "SUPPLIERS", expires: EPOCH_AT + 12 * DAY }),
      expiring({ kind: "certificate", server: "nats-eu-0", listener: "client", expires: EPOCH_AT + 16 * DAY }),
    ],
    expiringTotal: 3,
    expireLater: 26,
    neverExpire: 3,
  };
}

export function prod(): AccountsAndAuth {
  return {
    collected: true,
    epochAt: EPOCH_AT,
    operatorMode: false,
    accounts: [account("APP", { connections: 38, subscriptions: 220, jsStorage: 2 * GIB, streams: 6 })],
    accountsTotal: 1,
    failedLogins: [],
    failedLoginGroupsTotal: 0,
    failedLoginsTotal: 0,
    failedLoginsWindowStart: EPOCH_AT - DAY,
    captureGaps: false,
    expiring: [],
    expiringTotal: 0,
    expireLater: 6,
    neverExpire: 0,
  };
}

export function empty(): AccountsAndAuth {
  return {
    collected: false,
    operatorMode: false,
    accounts: [],
    accountsTotal: 0,
    failedLogins: [],
    failedLoginGroupsTotal: 0,
    failedLoginsTotal: 0,
    captureGaps: false,
    expiring: [],
    expiringTotal: 0,
    expireLater: 0,
    neverExpire: 0,
  };
}
