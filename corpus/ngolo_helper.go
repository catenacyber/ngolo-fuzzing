package main

import (
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
)

func NgoloCorpusMarshal(item interface{}) {
	ngolo_list := &NgoloFuzzOne{Item: item.(isNgoloFuzzOne_Item)}
	ngolo_fuzz := NgoloFuzzList{List: []*NgoloFuzzOne{ngolo_list}}
	data, _ := proto.Marshal(ngolo_fuzz)
	fname := filepath.Join(os.Getenv("FUZZ_NG_CORPUS_DIR"), string(sha1.Sum(data)))
	os.WriteFile(fname, data, 0644)
}
