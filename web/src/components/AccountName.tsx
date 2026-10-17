// An account shown by its readable name, with its ID as the tooltip and
// as text for screen readers.
export function AccountName({ id, name, label }: { id: string; name?: string; label?: string }) {
  const text = label ?? (name || id);
  return (
    <span title={id}>
      {text}
      {text !== id && <span className="sr-only">{` (account ID ${id})`}</span>}
    </span>
  );
}
