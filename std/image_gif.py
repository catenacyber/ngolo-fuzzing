#python
import sys

# special patch as gif.Decode needs to be checked by gif.DecodeConfig for arbitrary allocations
patch='''			arg1 := bytes.NewReader(a.Decode.R)
			cfg, err := gif.DecodeConfig(arg1)
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
    if "case *NgoloFuzzOne_Decode:" in l and patched < 2:
        print(patch)
        patched = patched + 1
    if "case *NgoloFuzzOne_DecodeAll:" in l and patched < 2:
        print(patch.replace('Decode.R', 'DecodeAll.R'))
        patched = patched + 1
