import {describe, expect, it} from 'vitest';
import {clientCells, cellProblems, cellTotal} from './clientLane';

const b = (index: number, connects: number, drops = 0) => ({index, connects, closes: connects, drops, shutdowns: 0, authErrors: 0});
const MIN = 60_000;

describe('clientCells', () => {
  it('keeps one cell per bucket when the track is wide enough', () => {
    const cells = clientCells([b(0, 3), b(2, 1, 2)], 0, MIN, 4, 4 * MIN, 400);
    expect(cells.map((c) => [c.from, c.to, cellTotal(c), cellProblems(c)])).toEqual([[0, MIN, 6, 0], [2 * MIN, 3 * MIN, 4, 2]]);
  });
  it('merges adjacent buckets on a narrow track and sums them', () => {
    const cells = clientCells([b(0, 3), b(1, 1, 1), b(5, 2)], 0, MIN, 6, 6 * MIN, 24, 12);
    expect(cells.map((c) => [c.from, c.to, c.buckets, c.connects, c.drops])).toEqual([[0, 3 * MIN, 3, 4, 1], [3 * MIN, 6 * MIN, 3, 2, 0]]);
  });
  it('ends the last cell at the range end and ignores buckets outside it', () => {
    const cells = clientCells([b(1, 1), b(9, 5)], 0, MIN, 2, 90_000, 0);
    expect(cells).toHaveLength(1);
    expect(cells[0].to).toBe(90_000);
  });
});
