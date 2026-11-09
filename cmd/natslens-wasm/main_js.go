//go:build js && wasm

package main

import (
	"log/slog"
	"os"
	"syscall/js"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	mux, err := newMux(js.Global().Get("natslensLive").Truthy(), log)
	if err != nil {
		panic(err)
	}
	js.Global().Set("natslensFetch", js.FuncOf(func(_ js.Value, args []js.Value) any {
		r := request{Method: args[0].String(), URL: args[1].String(), Header: map[string]string{}}
		if hdrs := args[2]; hdrs.Type() == js.TypeObject {
			keys := js.Global().Get("Object").Call("keys", hdrs)
			for i := range keys.Length() {
				k := keys.Index(i).String()
				r.Header[k] = hdrs.Get(k).String()
			}
		}
		if len(args) > 3 && args[3].Type() == js.TypeObject {
			r.Body = make([]byte, args[3].Get("length").Int())
			js.CopyBytesToGo(r.Body, args[3])
		}
		var executor js.Func
		executor = js.FuncOf(func(_ js.Value, p []js.Value) any {
			resolve, reject := p[0], p[1]
			// A request runs on its own goroutine: the call that started
			// it must return before Go may wait on anything.
			go func() {
				defer executor.Release()
				res, err := serve(mux, r)
				if err != nil {
					reject.Invoke(js.Global().Get("Error").New(err.Error()))
					return
				}
				hdr := js.Global().Get("Object").New()
				for k, v := range res.Header {
					hdr.Set(k, v)
				}
				body := js.Global().Get("Uint8Array").New(len(res.Body))
				js.CopyBytesToJS(body, res.Body)
				out := js.Global().Get("Object").New()
				out.Set("status", res.Status)
				out.Set("headers", hdr)
				out.Set("body", body)
				resolve.Invoke(out)
			}()
			return nil
		})
		return js.Global().Get("Promise").New(executor)
	}))
	if ready := js.Global().Get("natslensOnReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}
