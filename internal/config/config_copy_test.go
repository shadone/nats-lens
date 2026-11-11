package config

import (
	"reflect"
	"slices"
	"testing"
)

// decodeFileCopies are the maps and pointers of Config that decodeFile
// copies before the decoder writes into them.
var decodeFileCopies = []string{"Checks.Thresholds", "Checks.LoopbackIsTooling"}

// sharedFields lists the map, pointer and interface fields of t reachable
// from a Config value without passing through a slice or an array: the
// decoder writes into those of the base, while it builds slices anew.
func sharedFields(t reflect.Type, path string, out *[]string) {
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		p := f.Name
		if path != "" {
			p = path + "." + f.Name
		}
		switch f.Type.Kind() {
		case reflect.Map, reflect.Pointer, reflect.Interface:
			*out = append(*out, p)
		case reflect.Struct:
			sharedFields(f.Type, p, out)
		}
	}
}

// A new map or pointer in Config that decodeFile does not copy would let a
// file change the base it is decoded over.
func TestDecodeFileCopiesEverySharedField(t *testing.T) {
	var found []string
	sharedFields(reflect.TypeFor[Config](), "", &found)
	for _, p := range found {
		if !slices.Contains(decodeFileCopies, p) {
			t.Errorf("Config.%s is a map or pointer that decodeFile does not copy", p)
		}
	}
	for _, p := range decodeFileCopies {
		if !slices.Contains(found, p) {
			t.Errorf("decodeFileCopies names %s, which is not a map or pointer of Config", p)
		}
	}
}
