package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"sourcegraph.com/sourcegraph/srclib/graph"
)

func init() {
	_, err := flagParser.AddCommand("fmt",
		"format a Go object (def, doc)",
		"The fmt command takes an object and formats it.",
		&fmtCmd,
	)
	if err != nil {
		log.Fatal(err)
	}
}

type FmtCmd struct {
	UnitType   string `short:"u" long:"unit-type" description:"Unit type" required:"yes"`
	ObjectType string `short:"t" long:"object-type" description:"Object type ('def', 'doc')" required:"yes"`
	Format     string `short:"f" long:"format" description:"Format to output ('full', 'decl')" default:"full"`

	Object string `long:"object" description:"Object to format, serialized as JSON" required:"yes"`
}

var fmtCmd FmtCmd

func (c *FmtCmd) Execute(args []string) error {
	switch c.ObjectType {
	case "def":
		var d *graph.Def
		if err := json.Unmarshal([]byte(c.Object), &d); err != nil {
			return err
		}
		f := d.Fmt()
		fmt.Printf("%s %s%s%s", f.Kind(), f.Name(graph.Unqualified), f.NameAndTypeSeparator(), f.Type(graph.Unqualified))
		return nil
	case "doc":
		return errors.New("Doc formatting yet to be implemented")
	default:
		return errors.New("Object type not recognized: %s")
	}
}
