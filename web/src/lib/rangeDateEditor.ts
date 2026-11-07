/** Browser-locale presentation and strict Gregorian/UTC entry, independent of OS native-input formatting. */
export function createRangeDateEditor(locales: string[]) {
  const formatter = new Intl.DateTimeFormat(locales, {
    calendar: "gregory", timeZone: "UTC", year: "numeric", month: "numeric", day: "numeric", hour: "numeric", minute: "2-digit",
  });
  const { locale, numberingSystem, hourCycle } = formatter.resolvedOptions();
  const numbers = new Intl.NumberFormat(locale, { numberingSystem, useGrouping: false });
  const digits = Array.from({ length: 10 }, (_, n) => numbers.format(n));
  const normalize = (s: string) => {
    let text = s;
    digits.forEach((digit, n) => { text = text.replaceAll(digit, String(n)); });
    return text.replace(/[\u200e\u200f\u061c]/g, "").replace(/\s+/gu, " ").trim().toLowerCase();
  };
  const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const sample = Date.UTC(2006, 10, 22, 13, 45);
  const period = (hour: number) => normalize(formatter.formatToParts(Date.UTC(2006, 10, 22, hour)).find((p) => p.type === "dayPeriod")?.value ?? "");
  const am = period(1), pm = period(13);
  const fields: string[] = [];
  const pattern = formatter.formatToParts(sample).map((part) => {
    if (part.type === "literal") return escape(normalize(`x${part.value}x`).slice(1, -1)).replaceAll(" ", "\\s+");
    fields.push(part.type);
    return part.type === "dayPeriod" ? `(${escape(am)}|${escape(pm)})` : part.type === "year" ? "([0-9]{4})" : "([0-9]{1,2})";
  }).join("");
  const expression = new RegExp(`^${pattern}$`);

  return {
    format: (ms: number) => formatter.format(ms),
    example: formatter.format(sample),
    parse(value: string): number {
      // ASVS 2.2.1/2.2.3: allow only the displayed locale's structure and real calendar/time values.
      if (value.length > 80) return NaN;
      const match = expression.exec(normalize(value));
      if (!match) return NaN;
      const parts = Object.fromEntries(fields.map((field, i) => [field, match[i + 1]]));
      const year = Number(parts.year), month = Number(parts.month), day = Number(parts.day), minute = Number(parts.minute);
      let hour = Number(parts.hour);
      if (year < 1 || month < 1 || month > 12 || day < 1 || day > 31 || minute > 59) return NaN;
      if (am) {
        if (hour < (hourCycle === "h11" ? 0 : 1) || hour > (hourCycle === "h11" ? 11 : 12)) return NaN;
        hour = hour % 12 + (parts.dayPeriod === pm ? 12 : 0);
      } else {
        if (hour < (hourCycle === "h24" ? 1 : 0) || hour > (hourCycle === "h24" ? 24 : 23)) return NaN;
        hour %= 24;
      }
      const date = new Date(0);
      date.setUTCFullYear(year, month - 1, day);
      date.setUTCHours(hour, minute, 0, 0);
      return date.getUTCFullYear() === year && date.getUTCMonth() === month - 1 && date.getUTCDate() === day ? date.getTime() : NaN;
    },
  };
}
