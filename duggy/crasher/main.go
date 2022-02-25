package main

import (
	"github.com/catenacyber/ngolo-fuzzing/duggy"
)

func main() {
	p := duggy.CreateProcessor(42)
	duggy.Process(p, []byte{'B', 'U', 'G', 'Z'})
}
