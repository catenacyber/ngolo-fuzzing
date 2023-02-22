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
	fname := filepath.Join("corpus", string(sha1.Sum(data)))
	os.WriteFile(fname, data, 0644)
}
