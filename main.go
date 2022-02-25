package main

import (
	"flag"
	"log"

	"github.com/catenacyber/ngolo-fuzzing/pkgtofuzzinput"
)

var exclude = flag.String("exclude", "", "comma-separated string pattern to exclude from functions")

func main() {
	flag.Parse()

	if len(flag.Args()) < 1 {
		log.Fatalf("Expects a golang package name")
	}
	path := flag.Args()[0]
	outdir := "fuzz_ng"
	if len(flag.Args()) > 1 {
		outdir = flag.Args()[1]
	} else {
		log.Printf("Default to outdir in %s", outdir)
	}
	err := pkgtofuzzinput.PackageToFuzzer(path, outdir, *exclude)
	if err != nil {
		log.Fatalf("Failed creating fuzz target : %s", err)
	}
}
