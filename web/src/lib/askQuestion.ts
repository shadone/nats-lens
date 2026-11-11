// The command bar's Ask mode: a question for the user's own agent. The
// question goes into a copied prompt, never to a model or service.

/** The prefix that turns the command bar's text into a question. */
export const ASK_PREFIX = "ask:";

/** The longest question, in characters, after trimming. */
export const ASK_MAX = 2000;

const ASK_START = /^\s*ask:/i;

/**
 * The question when the text starts with ask:, the rest as typed and
 * trimmed; undefined otherwise. Other prefixes after ask: are part of the
 * question, not search prefixes.
 */
export function askText(raw: string): string | undefined {
  const m = ASK_START.exec(raw);
  return m ? raw.slice(m[0].length).trim() : undefined;
}

/** The text with a leading ask: taken out, for the chip's remove button. */
export function withoutAsk(raw: string): string {
  const q = askText(raw);
  return q === undefined ? raw : q;
}

export type QuestionCheck = { question: string; problem?: undefined } | { question?: undefined; problem: "empty" | "long"; length: number };

/** The trimmed question, or why it cannot be asked: empty, or longer than ASK_MAX characters. */
export function checkQuestion(raw: string): QuestionCheck {
  const question = raw.trim();
  const length = [...question].length;
  if (length === 0) return { problem: "empty", length };
  if (length > ASK_MAX) return { problem: "long", length };
  return { question };
}
