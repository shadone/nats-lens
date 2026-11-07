// The Overview's streams: worst first, each with one placement square per
// replica, its messages, size and growth per day.
import { STREAMS_SHOWN, type StreamPlacement } from "../api/overview";
import { fmtBytes, fmtGrowth } from "../lib/format";
import { streamLabel, LEVEL_ORDER, streamPath } from "../lib/view";
import { Num, RLink } from "./common";
import { PlacementLegend, PlacementSquares } from "./PlacementSquares";
import { HealthIcon } from "./Severity";

/** The worst streams, and a link to all when more than those match (count). */
export function StreamPlacementCard({ streams, count }: { streams: StreamPlacement[]; count: number }) {
  const ss = [...streams].sort((a, b) => LEVEL_ORDER[a.status] - LEVEL_ORDER[b.status]).slice(0, STREAMS_SHOWN);
  return (
    <>
      <div className="table-wrap">
        <table className="tbl sp">
          <thead>
            <tr>
              <th scope="col">Stream</th>
              <th scope="col">Placement</th>
              <th scope="col" className="num">
                Msgs
              </th>
              <th scope="col" className="num">
                Size
              </th>
              <th scope="col" className="num">
                /day
              </th>
            </tr>
          </thead>
          <tbody>
            {ss.map((s) => (
              <tr key={s.entity.key}>
                <td>
                  <span className="sp-n">
                    <HealthIcon level={s.status} />
                    <RLink to={streamPath(s.entity.account, s.entity.stream)} className="x mono">
                      {streamLabel(s.entity.stream)}
                    </RLink>
                  </span>
                </td>
                <td>
                  <PlacementSquares placement={s.placement} />
                </td>
                <td className="num">
                  <Num v={s.messages} />
                </td>
                <td className="num">
                  <Num v={s.bytes} fmt={fmtBytes} />
                </td>
                <td className="num muted">
                  <Num v={s.bytesPerDay} fmt={fmtGrowth} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <PlacementLegend states={ss.flatMap((s) => s.placement.map((p) => p.state))} />
      {count > ss.length && (
        <RLink to="/streams" className="x more-link">
          All {count} streams
        </RLink>
      )}
    </>
  );
}
