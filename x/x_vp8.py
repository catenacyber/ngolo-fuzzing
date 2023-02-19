#python
import sys

# special patch as vp8.Decode needs to be checked by vp8.DecodeConfig for arbitrary allocations
patch='''			arg1 := bytes.NewReader(a.Decode.R)
			cfg, err := vp8.DecodeConfig(arg1)
			if err != nil {
				return 0
			}
			if cfg.Width * cfg.Height > 1024*1024 {
				continue
			}'''

f = open(sys.argv[1])
patched = 0
for l in f.readlines():
    print(l, end="")
    if "case *NgoloFuzzOne_Decode:" in l and patched < 1:
        print(patch)
        patched = patched + 1
