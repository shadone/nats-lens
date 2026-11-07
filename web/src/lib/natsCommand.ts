// nats CLI commands for a person to copy and run by hand. Every name goes
// after `--`, so a name that starts with "-" is never read as an option,
// and a name with a control character gets no command at all, since a
// newline would start a second command in a shell.

/** Whether a name holds a control character (newline, CR, NUL and the like). */
export function hasControl(s: string): boolean {
  return /[\u0000-\u001f\u007f]/.test(s);
}

/** A name as one shell word: bare when plain, else in single quotes. */
export function shellWord(s: string): string {
  return /^[A-Za-z0-9._][A-Za-z0-9._-]*$/.test(s) ? s : `'${s.replace(/'/g, `'\\''`)}'`;
}

/**
 * "nats <words> <flags> -- <names>", or undefined when a name holds a
 * control character.
 */
export function natsCommand(words: string[], names: string[], flags: string[] = []): string | undefined {
  if (names.some(hasControl)) return undefined;
  return ["nats", ...words, ...flags, "--", ...names.map(shellWord)].join(" ");
}
