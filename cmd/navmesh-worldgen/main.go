// Command navmesh-worldgen writes deterministic qualification source documents.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/demiurgos-hub/navmesh-go/qualification/worldgen"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	c := worldgen.DefaultConfig()
	in := flag.String("config", "", "generator JSON config")
	out := flag.String("out", "", "new output directory (must not already exist)")
	flag.Parse()
	if *out == "" {
		return fmt.Errorf("-out is required")
	}
	if *in != "" {
		f, e := os.Open(*in)
		if e != nil {
			return e
		}
		defer f.Close()
		d := json.NewDecoder(f)
		d.DisallowUnknownFields()
		if e = d.Decode(&c); e != nil {
			return e
		}
	}
	f, e := worldgen.Generate(c)
	if e != nil {
		return e
	}
	if e = os.Mkdir(*out, 0755); e != nil {
		return e
	}
	files := map[string]string{}
	write := func(name string, v any) error {
		data, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			return e
		}
		data = append(data, '\n')
		if e = os.WriteFile(filepath.Join(*out, name), data, 0644); e != nil {
			return e
		}
		h := sha256.Sum256(data)
		files[name] = hex.EncodeToString(h[:])
		return nil
	}
	if e = write("fixture.json", f); e != nil {
		return e
	}
	for _, p := range f.Profiles {
		if e = write(p.Profile.ID+".source.json", worldgen.Source{Generator: "navmesh-worldgen v1", Scene: f.Scene, Config: p}); e != nil {
			return e
		}
	}
	data, e := json.MarshalIndent(files, "", "  ")
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(*out, "sha256.json"), append(data, '\n'), 0644); e != nil {
		return e
	}
	fmt.Printf("generated %s half_extent=%g seed=%d at %s\n", c.Family, c.HalfExtent, c.Seed, *out)
	return nil
}
