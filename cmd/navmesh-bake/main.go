// Command navmesh-bake bakes a JSON source/configuration document using Go only.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/demiurgos-hub/navmesh-go/artifact"
	"github.com/demiurgos-hub/navmesh-go/bake"
	"github.com/demiurgos-hub/navmesh-go/geom"
)

type request struct {
	Generator string `json:",omitempty"`
	Scene     geom.Scene
	Config    bake.Config
}

func main() {
	input := flag.String("in", "", "JSON source/configuration file")
	output := flag.String("out", "", "portable navigation bundle")
	flag.Parse()
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: navmesh-bake -in scene.json -out world.nav")
		os.Exit(2)
	}
	if err := run(*input, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(input, output string) error {
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := io.LimitReader(f, 256<<20+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if len(data) > 256<<20 {
		return fmt.Errorf("source exceeds 256 MiB")
	}
	var req request
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&req); err != nil {
		return err
	}
	if err = dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	result, err := bake.Build(ctx, req.Scene, req.Config)
	if err != nil {
		return err
	}
	encoded, err := artifact.Encode(result.Bundle)
	if err != nil {
		return err
	}
	dir := filepath.Dir(output)
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".navmesh-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err = temp.Write(encoded); err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, output); err != nil {
		return err
	}
	return json.NewEncoder(os.Stderr).Encode(struct {
		World        string
		Tiles, Bytes int
		Diagnostics  []bake.TileDiagnostic
	}{result.Bundle.WorldID, len(result.Bundle.Tiles), len(encoded), result.Diagnostics})
}
