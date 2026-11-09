//go:build js && wasm

package main

import (
	"errors"
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
		r, err := requestFromJS(args)
		if err != nil {
			return js.Global().Get("Promise").Call("reject", js.Global().Get("Error").New(err.Error()))
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

// requestFromJS reads natslensFetch's arguments: method and URL strings, a
// headers object and a Uint8Array body or null. A call that does not fit
// gets an error, which rejects its promise, rather than a panic, which would
// end the program every later call needs.
func requestFromJS(args []js.Value) (request, error) {
	if len(args) < 3 || args[0].Type() != js.TypeString || args[1].Type() != js.TypeString {
		return request{}, errors.New("natslensFetch(method, url, headers, body): method and url must be strings")
	}
	r := request{Method: args[0].String(), URL: args[1].String(), Header: map[string]string{}}
	switch hdrs := args[2]; hdrs.Type() {
	case js.TypeObject:
		keys := js.Global().Get("Object").Call("keys", hdrs)
		for i := range keys.Length() {
			k := keys.Index(i).String()
			r.Header[k] = hdrs.Get(k).String()
		}
	case js.TypeNull, js.TypeUndefined:
	default:
		return request{}, errors.New("natslensFetch: headers must be an object")
	}
	if len(args) > 3 {
		switch b := args[3]; {
		case b.IsNull() || b.IsUndefined():
		case b.InstanceOf(js.Global().Get("Uint8Array")):
			r.Body = make([]byte, b.Get("length").Int())
			js.CopyBytesToGo(r.Body, b)
		default:
			return request{}, errors.New("natslensFetch: body must be a Uint8Array or null")
		}
	}
	return r, nil
}
