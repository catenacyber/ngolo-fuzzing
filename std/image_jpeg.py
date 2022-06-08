#python
import sys

# special patch as jpeg.Decode needs to be checked by jpeg.DecodeConfig for arbitrary allocations
patch='''			arg1 := bytes.NewReader(a.Decode.R)
			cfg, err := jpeg.DecodeConfig(arg1)
			if err != nil {
				return 0
			}
			if cfg.Width * cfg.Height > 1024*1024 {
				continue
			}'''

f = open(sys.argv[1])
patched = False
for l in f.readlines():
    print(l, end="")
    if "case *NgoloFuzzOne_Decode:" in l and not patched:
        print(patch)
        patched = True
