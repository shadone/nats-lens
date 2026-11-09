// The fetch the Connect transport uses in place of the browser's: none here.
// The demo site's build (VITE_NATSLENS_WASM=1) replaces this module with
// wasmTransport.ts (see vite.config.ts), so other builds hold none of its code.
export const demoFetch: typeof globalThis.fetch | undefined = undefined;
