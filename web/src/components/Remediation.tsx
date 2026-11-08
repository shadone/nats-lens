import Markdown, { defaultUrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";

/** Shared Markdown for check references and remediation, including GFM tables. */
export function Remediation({ text }: { text: string }) {
  return <div className="rem">
    <Markdown
      remarkPlugins={[remarkGfm]}
      // ASVS 1.2.1, 1.3.5: keep HTML inert and the parser's safe URL policy.
      // Guidance never needs to fetch remote images or accept URL controls.
      disallowedElements={["img"]}
      urlTransform={url => /[\u0000-\u001f\u007f]/.test(url) ? "" : defaultUrlTransform(url)}
      components={{
        table: ({ children }) => <div className="rem-table-scroll" role="region" aria-label="Scrollable table" tabIndex={0}><table>{children}</table></div>,
        th: ({ children, style }) => <th scope="col" style={style}>{children}</th>,
      }}
    >{text}</Markdown>
  </div>;
}

