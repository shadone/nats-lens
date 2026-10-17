// Who the server records as the author of a mute or unmute. GetLensInfo
// says how the server learns it: in IAP mode from the verified IAP header,
// ignoring any name in the request; in request mode from a name the request
// gives. Only this module knows the rule.
import type { LensInfo } from "./model";

export interface IdentityNeed {
  /** Whether the dialogs show a "Your name" field and require it. */
  askName: boolean;
  /** Whether the server records the IAP user. */
  iap: boolean;
}

/** Until the mode is known the dialogs ask, since request mode needs a name. */
export function identityNeed(info: LensInfo | undefined): IdentityNeed {
  const iap = info?.identityMode === "iap";
  return { askName: !iap, iap };
}

const NAME_KEY = "nats-lens-name";
/** Written by an earlier version that guessed IAP mode; no longer read. */
const LEGACY_IAP_KEY = "nats-lens-iap";

/** The last name typed, so the field is filled in next time. */
export function rememberedName(): string {
  try {
    return localStorage.getItem(NAME_KEY) ?? "";
  } catch {
    return "";
  }
}

export function rememberName(name: string): void {
  try {
    localStorage.setItem(NAME_KEY, name);
    localStorage.removeItem(LEGACY_IAP_KEY);
  } catch {
    // Storage blocked: the field is simply empty next time.
  }
}

/** Removes the flag an earlier version stored. */
export function clearLegacyIdentityFlag(): void {
  try {
    localStorage.removeItem(LEGACY_IAP_KEY);
  } catch {
    // Storage blocked: there is nothing stored either.
  }
}
