/**
 * Whether this is the demo site's build (VITE_NATSLENS_WASM=1): the static
 * site whose API runs in the page as WebAssembly, with made-up data. Other
 * builds leave out what depends on it.
 */
export const HOSTED_DEMO: boolean = import.meta.env.VITE_NATSLENS_WASM === "1";
