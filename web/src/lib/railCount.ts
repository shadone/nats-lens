// The collapsed sidebar's badge sits on its icon and has room for two digits;
// the expanded sidebar and the link's accessible name keep the full count.
export function railCount(count: number): string {
  return count > 99 ? "99+" : String(count);
}
