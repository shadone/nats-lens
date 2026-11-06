/** A selection drag must not activate the control whose text it selects. */
export function selectionIntersects(node: Node): boolean {
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed) return false;
  for (let i = 0; i < selection.rangeCount; i++) {
    if (selection.getRangeAt(i).intersectsNode(node)) return true;
  }
  return false;
}
