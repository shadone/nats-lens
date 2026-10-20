// Accounts and auth: GetAccountsAndAuthResponse to the view model of the
// Accounts page. The only file that reads its messages; a value the server
// left unset stays undefined here, never 0.
import { ExpiringKind, type GetAccountsAndAuthResponse } from "../gen/natslens/v1/accounts_pb";
import { ms, optN } from "./adapt-common";

export interface AccountRow {
  /** The account public key in operator mode, the configured name in config mode. */
  account: string;
  accountName: string;
  isSystem: boolean;
  connections?: number;
  leafnodes?: number;
  subscriptions?: number;
  jsStorage?: number;
  jsMemory?: number;
  streams?: number;
  /** -1 no limit; undefined without a known account JWT. */
  connLimit?: number;
  /** Bytes; -1 no limit, 0 JetStream not enabled; undefined without a known account JWT. */
  diskLimit?: number;
  imports?: number;
  exports?: number;
  /** Looked up at most 24 hours before the collection; without it the limits and JWT fields are unset. */
  detailsKnown: boolean;
  /** Undefined when the JWT never expires, there is none, or the account was not looked up. */
  jwtExpires?: number;
  hasJwt: boolean;
  expired: boolean;
  detailsAt?: number;
}

export interface FailedLogin {
  /** Empty when the client gave no user. */
  user: string;
  host: string;
  reason: string;
  account: string;
  accountName: string;
  count: number;
  last?: number;
}

export type ExpiringKindName = "certificate" | "accountJwt" | "userJwt" | "unknown";

export interface Expiring {
  kind: ExpiringKindName;
  server: string;
  listener: string;
  account: string;
  accountName: string;
  user: string;
  /** Undefined for an account JWT the server reports expired without a time. */
  expires?: number;
  expired: boolean;
  connections: number;
}

export interface AccountsAndAuth {
  /** False when the range holds no collection; everything else is then empty. */
  collected: boolean;
  epochAt?: number;
  operatorMode: boolean;
  accounts: AccountRow[];
  accountsTotal: number;
  failedLogins: FailedLogin[];
  failedLoginGroupsTotal: number;
  failedLoginsTotal: number;
  failedLoginsWindowStart?: number;
  captureGaps: boolean;
  expiring: Expiring[];
  expiringTotal: number;
  expireLater: number;
  neverExpire: number;
}

const KIND: Record<ExpiringKind, ExpiringKindName> = {
  [ExpiringKind.UNSPECIFIED]: "unknown",
  [ExpiringKind.SERVER_CERTIFICATE]: "certificate",
  [ExpiringKind.ACCOUNT_JWT]: "accountJwt",
  [ExpiringKind.USER_JWT]: "userJwt",
};

export function accountsAndAuth(r: GetAccountsAndAuthResponse): AccountsAndAuth {
  return {
    collected: r.epoch !== 0n,
    epochAt: ms(r.epochAt),
    operatorMode: r.operatorMode,
    accounts: r.accounts.map((a) => ({
      account: a.account,
      accountName: a.accountName,
      isSystem: a.isSystem,
      connections: a.connections,
      leafnodes: a.leafnodes,
      subscriptions: optN(a.subscriptions),
      jsStorage: optN(a.jsStorage),
      jsMemory: optN(a.jsMemory),
      streams: a.streams,
      connLimit: optN(a.connLimit),
      diskLimit: optN(a.diskLimit),
      imports: a.imports,
      exports: a.exports,
      detailsKnown: a.detailsKnown,
      jwtExpires: ms(a.jwtExpires),
      hasJwt: a.hasJwt,
      expired: a.expired,
      detailsAt: ms(a.detailsAt),
    })),
    accountsTotal: r.accountsTotal,
    failedLogins: r.failedLogins.map((l) => ({
      user: l.user,
      host: l.host,
      reason: l.reason,
      account: l.account,
      accountName: l.accountName,
      count: Number(l.count),
      last: ms(l.last),
    })),
    failedLoginGroupsTotal: r.failedLoginGroupsTotal,
    failedLoginsTotal: Number(r.failedLoginsTotal),
    failedLoginsWindowStart: ms(r.failedLoginsWindowStart),
    captureGaps: r.captureGaps,
    expiring: r.expiring.map((e) => ({
      kind: KIND[e.kind] ?? "unknown",
      server: e.server,
      listener: e.listener,
      account: e.account,
      accountName: e.accountName,
      user: e.user,
      expires: ms(e.expires),
      expired: e.expired,
      connections: e.connections,
    })),
    expiringTotal: r.expiringTotal,
    expireLater: r.expireLater,
    neverExpire: r.neverExpire,
  };
}
