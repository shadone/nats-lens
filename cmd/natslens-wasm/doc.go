// Command natslens-wasm runs the API of natslens demo inside a web page, for
// the static demo site; it builds only with GOOS=js GOARCH=wasm. It sets
// globalThis.natslensFetch(method, url, headers, body), which returns a
// Promise of {status, headers, body}; the page's Connect transport calls it
// in place of the network. The page sets globalThis.natslensLive before it
// starts the program, and the program calls globalThis.natslensOnReady once
// natslensFetch is set.
package main
