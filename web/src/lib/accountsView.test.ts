import { describe, expect, it } from "vitest";
import { account, EPOCH_AT, expiring, global, login, prod, SUPPLIERS } from "../test/fixtures/accounts";
import {
  connLimitText,
  diskLimitText,
  diskStateText,
  expiresIn,
  expiringCountLine,
  expiringTone,
  expiringWhat,
  headerLine,
  importsExports,
  isIdle,
  jetStreamOff,
  jwtCell,
  limitBar,
  limitBarText,
  memoryOnly,
  loginHot,
  loginRateText,
  loginUser,
  loginWindowMs,
  loginWindowShort,
  loginsCutLine,
  orderAccounts,
  seenAtText,
  shortIdentifier,
} from "./accountsView";

const DAY = 86_400_000;
const GIB = 1024 ** 3;

describe("headerLine", () => {
  it("names the mode, the count and where names come from", () => {
    expect(headerLine(global())).toBe("Operator mode · 3 accounts · names from the account JWTs");
    expect(headerLine({ ...prod(), accountsTotal: 2 })).toBe("Config mode · 2 accounts · names from the server configuration");
    expect(headerLine(prod())).toBe("Config mode · 1 account · names from the server configuration");
  });

  it("counts every account, not only those listed", () => {
    expect(headerLine({ ...global(), accountsTotal: 612 })).toContain("612 accounts");
  });
});

describe("orderAccounts", () => {
  it("puts the system account last, the others by connections, most first, then by name", () => {
    const rows = [
      account("SYS", { isSystem: true, connections: 900 }),
      account("B", { connections: 5 }),
      account("C", { connections: 40 }),
      account("A", { connections: 5 }),
      account("D", { connections: undefined }),
    ];
    expect(orderAccounts(rows).map((a) => a.account)).toEqual(["C", "A", "B", "D", "SYS"]);
  });

  it("sorts by the readable name, and by code unit as the server does", () => {
    const rows = [account("AKEY2", { accountName: "beta" }), account("AKEY1", { accountName: "Zulu" }), account("AKEY3", { accountName: "" })];
    expect(orderAccounts(rows).map((a) => a.account)).toEqual(["AKEY3", "AKEY1", "AKEY2"]);
  });
});

describe("shortIdentifier", () => {
  it("shortens an account key under its name", () => {
    expect(shortIdentifier({ account: SUPPLIERS, accountName: "SUPPLIERS" })).toBe("ADSU…RT5NH");
  });

  it("shows nothing when the identifier is the name, as in config mode", () => {
    expect(shortIdentifier({ account: "APP", accountName: "APP" })).toBeUndefined();
    expect(shortIdentifier({ account: SUPPLIERS, accountName: "" })).toBeUndefined();
  });

  it("keeps an identifier that is not a key whole", () => {
    expect(shortIdentifier({ account: "shop", accountName: "Shop" })).toBe("shop");
  });
});

describe("limitBar", () => {
  it("draws a bar only against a limit above 0", () => {
    expect(limitBar(12, undefined)).toBeUndefined();
    expect(limitBar(12, -1)).toBeUndefined();
    expect(limitBar(0, 0)).toBeUndefined();
    expect(limitBar(undefined, 2000)).toBeUndefined();
  });

  it("fills to the share of the limit, in the accent below 80% and the warning colour from it", () => {
    expect(limitBar(412, 2000)).toEqual({ pct: 20.6, share: 20.6, tone: "ok" });
    expect(limitBar(1599, 2000)?.tone).toBe("ok");
    expect(limitBar(1600, 2000)).toEqual({ pct: 80, share: 80, tone: "warn" });
    expect(limitBar(2000, 2000)?.tone).toBe("warn");
    expect(limitBar(410 * GIB, 500 * GIB)?.tone).toBe("warn");
  });

  it("stops at a full bar over the limit, keeps the real share, and is critical", () => {
    expect(limitBar(2500, 2000)).toEqual({ pct: 100, share: 125, tone: "crit" });
  });

  it("words the share, and says when it is over the limit", () => {
    expect(limitBarText(limitBar(412, 2000)!)).toBe("20% of the limit");
    expect(limitBarText(limitBar(1640, 2000)!)).toBe("82% of the limit");
    expect(limitBarText(limitBar(75, 50)!)).toBe("150% of the limit, over the limit");
    expect(limitBarText(limitBar(0, 50)!)).toBe("0% of the limit");
  });
});

describe("limits in words", () => {
  it("says the connection limit, unlimited, or nothing", () => {
    expect(connLimitText(2000)).toBe("of 2,000");
    expect(connLimitText(-1)).toBe("of unlimited");
    expect(connLimitText(undefined)).toBeUndefined();
  });

  it("says the disk limit, unlimited, or nothing", () => {
    expect(diskLimitText(account("APP", { diskLimit: 500 * GIB }))).toBe("of 500 GiB");
    expect(diskLimitText(account("APP", { diskLimit: -1 }))).toBe("of unlimited");
    expect(diskLimitText(account("APP", { diskLimit: undefined }))).toBeUndefined();
  });

  it("reads JetStream off from jetstream_enabled false", () => {
    const off = account("SYS", { diskLimit: 0, jetstreamEnabled: false });
    expect(jetStreamOff(off)).toBe(true);
    expect(memoryOnly(off)).toBe(false);
    expect(diskStateText(off)).toBe("JetStream off");
    expect(diskLimitText(off)).toBeUndefined();
  });

  it("reads memory only from JetStream enabled with a disk limit of 0, never as 'of 0 B'", () => {
    const mem = account("APP", { diskLimit: 0, jetstreamEnabled: true });
    expect(jetStreamOff(mem)).toBe(false);
    expect(memoryOnly(mem)).toBe(true);
    expect(diskStateText(mem)).toBe("memory only");
    expect(diskLimitText(mem)).toBeUndefined();
  });

  it("says nothing of an enabled account with file storage", () => {
    const on = account("APP", { diskLimit: 500 * GIB, jetstreamEnabled: true });
    expect(jetStreamOff(on)).toBe(false);
    expect(memoryOnly(on)).toBe(false);
    expect(diskStateText(on)).toBeUndefined();
    expect(diskStateText(account("APP", { diskLimit: -1, jetstreamEnabled: true }))).toBeUndefined();
  });

  it("calls nothing off without a known JWT, whatever the disk limit", () => {
    for (const diskLimit of [undefined, 0]) {
      const unknown = account("APP", { diskLimit, jetstreamEnabled: undefined });
      expect(jetStreamOff(unknown)).toBe(false);
      expect(memoryOnly(unknown)).toBe(false);
      expect(diskStateText(unknown)).toBeUndefined();
    }
  });
});

describe("importsExports", () => {
  it("counts imports and exports, or says none", () => {
    expect(importsExports(account("APP", { connections: 4, imports: 2, exports: 1 }))).toBe("2 imports · 1 export");
    expect(importsExports(account("APP", { connections: 4, imports: 1, exports: 0 }))).toBe("1 import");
    expect(importsExports(account("APP", { connections: 4 }))).toBe("none");
  });

  it("calls the system account the system account", () => {
    expect(importsExports(account("SYS", { isSystem: true, exports: 11 }))).toBe("system account");
  });

  it("marks an account with no connections, leaf nodes or streams idle now", () => {
    const idle = account(SUPPLIERS, { accountName: "SUPPLIERS", exports: 1 });
    expect(isIdle(idle)).toBe(true);
    expect(importsExports(idle)).toBe("1 export · idle now");
    expect(importsExports(account("X"))).toBe("idle now");
  });

  it("does not call an account idle on counts that were not reported", () => {
    expect(isIdle(account("X", { connections: undefined }))).toBe(false);
    expect(isIdle(account("X", { streams: undefined }))).toBe(false);
    expect(isIdle(account("X", { leafnodes: 2 }))).toBe(false);
  });

  it("says visibly that an account was not looked up yet, in either mode", () => {
    expect(importsExports(account("APP", { connections: 4, detailsKnown: false, imports: undefined, exports: undefined }))).toBe("not looked up yet");
    expect(importsExports(account("APP", { connections: 0, streams: 0, detailsKnown: false, imports: undefined, exports: undefined }))).toBe("not looked up yet · idle now");
  });

  it("shows the count it has when the other is not reported", () => {
    expect(importsExports(account("APP", { connections: 3, imports: undefined, exports: 2 }))).toBe("2 exports");
    expect(importsExports(account("APP", { connections: 3, imports: undefined, exports: undefined }))).toBeUndefined();
  });
});

describe("jwtCell", () => {
  const jwt = (o: Parameters<typeof account>[1]) => account("APP", { hasJwt: true, ...o });

  it("leaves the cell to a dash in config mode", () => {
    expect(jwtCell(account("APP"), false, EPOCH_AT)).toEqual({ why: "no account JWTs in config mode" });
  });

  it("leaves not looked up yet to a dash with the reason for screen readers; Imports / exports says it visibly", () => {
    expect(jwtCell(account("APP", { detailsKnown: false, hasJwt: false }), true, EPOCH_AT)).toEqual({ why: "not looked up yet" });
  });

  it("says never for a JWT without expiry, expired in critical, and the date otherwise", () => {
    expect(jwtCell(jwt({}), true, EPOCH_AT)).toEqual({ text: "never", tone: "muted" });
    expect(jwtCell(jwt({ expired: true }), true, EPOCH_AT)).toEqual({ text: "expired", tone: "crit" });
    expect(jwtCell(jwt({ jwtExpires: EPOCH_AT - 1000 }), true, EPOCH_AT)).toEqual({ text: "expired", tone: "crit" });
    expect(jwtCell(jwt({ jwtExpires: Date.parse("2026-10-29T14:30:00Z") }), true, EPOCH_AT)).toEqual({ text: "2026-10-29", tone: "warn", note: "in 12 days" });
    expect(jwtCell(jwt({ jwtExpires: EPOCH_AT + 90 * DAY }), true, EPOCH_AT)).toEqual({ text: "2027-01-15" });
  });

  it("gives a dash to a looked-up account without a JWT", () => {
    expect(jwtCell(account("APP"), true, EPOCH_AT)).toEqual({ why: "no account JWT" });
  });
});

describe("failed logins", () => {
  it("counts over 24 hours, or the shorter history", () => {
    expect(loginWindowMs({ epochAt: EPOCH_AT, failedLoginsWindowStart: EPOCH_AT - DAY })).toBe(DAY);
    expect(loginWindowMs({ epochAt: EPOCH_AT, failedLoginsWindowStart: EPOCH_AT - 2 * 3_600_000 })).toBe(2 * 3_600_000);
    expect(loginWindowMs({ epochAt: EPOCH_AT, failedLoginsWindowStart: undefined })).toBe(DAY);
  });

  it("reads a window that starts at the collection, the first one, as no history, not a day", () => {
    expect(loginWindowMs({ epochAt: EPOCH_AT, failedLoginsWindowStart: EPOCH_AT })).toBe(0);
    expect(loginWindowShort({ epochAt: EPOCH_AT, failedLoginsWindowStart: EPOCH_AT })).toBe(true);
    expect(loginWindowShort({ epochAt: EPOCH_AT, failedLoginsWindowStart: EPOCH_AT - DAY })).toBe(false);
    expect(loginWindowShort({ epochAt: EPOCH_AT, failedLoginsWindowStart: undefined })).toBe(false);
    expect(loginHot(login("", "", "", 70), 0)).toBe(true);
  });

  it("words a frequent group's rate", () => {
    expect(loginRateText(login("label-printer", "", "", 1_440), DAY)).toBe("about 60 an hour");
    expect(loginRateText(login("", "", "", 130), 2 * 3_600_000)).toBe("about 65 an hour");
  });

  it("warns of a group averaging 60 or more an hour", () => {
    expect(loginHot(login("label-printer", "", "", 1_440), DAY)).toBe(true);
    expect(loginHot(login("", "", "", 1_439), DAY)).toBe(false);
    expect(loginHot(login("", "", "", 96), DAY)).toBe(false);
    expect(loginHot(login("", "", "", 130), 2 * 3_600_000)).toBe(true);
  });

  it("rates a window under an hour by the true hourly rate, with a ten-minute floor", () => {
    // 30 in ten minutes is 180 an hour; 40 in 30 minutes is 80 an hour; 9 in ten minutes is 54.
    expect(loginHot(login("", "", "", 30), 600_000)).toBe(true);
    expect(loginHot(login("", "", "", 40), 1_800_000)).toBe(true);
    expect(loginHot(login("", "", "", 9), 600_000)).toBe(false);
    // The first collection (a window of 0) counts over ten minutes: 10 is 60 an hour, 8 is 48.
    expect(loginHot(login("", "", "", 10), 0)).toBe(true);
    expect(loginHot(login("", "", "", 8), 0)).toBe(false);
    expect(loginHot(login("", "", "", 1), 0)).toBe(false);
  });

  it("words the count since the window's start for a window under an hour", () => {
    expect(loginRateText(login("", "", "", 60), 1_800_000, EPOCH_AT - 1_800_000)).toBe("60 since 14:00 UTC");
    expect(loginRateText(login("", "", "", 70), 0, EPOCH_AT)).toBe("70 since 14:30 UTC");
    expect(loginRateText(login("", "", "", 1_440), DAY, EPOCH_AT - DAY)).toBe("about 60 an hour");
  });

  it("names a client without a user", () => {
    expect(loginUser(login("", "", "", 1))).toBe("(no user)");
    expect(loginUser(login("app", "", "", 1))).toBe("app");
  });

  it("says when groups were cut", () => {
    expect(loginsCutLine(global())).toBeUndefined();
    expect(loginsCutLine({ ...global(), failedLoginGroupsTotal: 140, failedLoginsTotal: 2_000 })).toBe(
      "The 2 most frequent of 140 groups are listed; 2,000 failed logins in all.",
    );
  });
});

describe("expiring", () => {
  it("words what expires", () => {
    expect(expiringWhat(expiring({ kind: "certificate", listener: "client" }))).toBe("certificate, client listener");
    expect(expiringWhat(expiring({ kind: "certificate", listener: "leafnode" }))).toBe("certificate, leaf node listener");
    expect(expiringWhat(expiring({ kind: "accountJwt" }))).toBe("account JWT");
    expect(expiringWhat(expiring({ kind: "userJwt" }))).toBe("user JWT");
  });

  it("says how long is left, or expired", () => {
    expect(expiresIn(expiring({ expires: EPOCH_AT + 16 * DAY + 3_600_000 }), EPOCH_AT)).toBe("16 days");
    expect(expiresIn(expiring({ expires: EPOCH_AT + DAY }), EPOCH_AT)).toBe("1 day");
    expect(expiresIn(expiring({ expires: EPOCH_AT + 5 * 3_600_000 + 12 * 60_000 }), EPOCH_AT)).toBe("5h 12m");
    expect(expiresIn(expiring({ expires: EPOCH_AT - DAY, expired: true }), EPOCH_AT)).toBe("expired");
    expect(expiresIn(expiring({ kind: "accountJwt", expired: true }), EPOCH_AT)).toBe("expired");
  });

  it("colours expired rows critical and the rest of the listed rows as warnings", () => {
    expect(expiringTone(expiring({ expires: EPOCH_AT + 16 * DAY }), EPOCH_AT)).toBe("warn");
    expect(expiringTone(expiring({ expires: EPOCH_AT - 1, expired: true }), EPOCH_AT)).toBe("crit");
    expect(expiringTone(expiring({ expires: EPOCH_AT - 1 }), EPOCH_AT)).toBe("crit");
    expect(expiringTone(expiring({ kind: "accountJwt", expired: true }), EPOCH_AT)).toBe("crit");
  });

  it("counts the rest", () => {
    expect(expiringCountLine(global())).toBe("26 more expire later; 3 never expire.");
    expect(expiringCountLine(prod())).toBe("6 expire later.");
    expect(expiringCountLine({ ...prod(), expireLater: 1, neverExpire: 1 })).toBe("1 expires later; 1 never expires.");
    expect(expiringCountLine({ ...prod(), expireLater: 0 })).toBeUndefined();
    expect(expiringCountLine({ ...global(), expiringTotal: 140 })).toBe("The 3 soonest of 140 are listed; 26 more expire later; 3 never expire.");
  });
});

describe("seenAtText", () => {
  it("says when an older certificate row was read, with the day when the UTC date differs from the collection's", () => {
    expect(seenAtText(expiring({ server: "nats-us-4", seenAt: EPOCH_AT - 70 * 60_000 }), EPOCH_AT)).toBe("read at 13:20 UTC; no answer from the server since");
    expect(seenAtText(expiring({ server: "nats-us-4", seenAt: EPOCH_AT - 25 * 3_600_000 }), EPOCH_AT)).toBe("read at 16 Oct 13:30 UTC; no answer from the server since");
    // Under 24 hours back but on the previous UTC day: 13:20 alone would read as after a 10:00 collection.
    expect(seenAtText(expiring({ seenAt: Date.parse("2026-10-16T13:20:00Z") }), Date.parse("2026-10-17T10:00:00Z"))).toBe("read at 16 Oct 13:20 UTC; no answer from the server since");
    expect(seenAtText(expiring({ server: "nats-us-4" }), EPOCH_AT)).toBeUndefined();
  });
});
