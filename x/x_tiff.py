#python
import sys

# special patch as tiff.Decode needs to be checked by tiff.DecodeConfig for arbitrary allocations
patch='''			arg1 := bytes.NewReader(a.Decode.R)
			cfg, err := tiff.DecodeConfig(arg1)
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
