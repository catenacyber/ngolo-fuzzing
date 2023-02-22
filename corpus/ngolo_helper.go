package main

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"

	"google.golang.org/protobuf/proto"
)

func NgoloCorpusMarshal(item interface{}) {
	ngolo_list := &NgoloFuzzOne{Item: item.(isNgoloFuzzOne_Item)}
	ngolo_fuzz := NgoloFuzzList{List: []*NgoloFuzzOne{ngolo_list}}
	data, _ := proto.Marshal(&ngolo_fuzz)
	hash := sha1.Sum(data)
	fname := filepath.Join(os.Getenv("FUZZ_NG_CORPUS_DIR"), string(hex.EncodeToString(hash[:])))
	os.WriteFile(fname, data, 0644)
}
