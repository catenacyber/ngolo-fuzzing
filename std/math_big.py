#python
import sys

# special patch as math/big.Float.Sqrt may panic with ErrNan
patch='''			if arg1.Sign() == -1 {
				continue
			}'''

f = open(sys.argv[1])
patched = 0
for l in f.readlines():
    if "case *NgoloFuzzOne_FloatNgdotSqrt:" in l and patched == 0:
        patched = 1
    if ".Sqrt(" in l and patched == 1:
        print(patch)
        patched = 2
    print(l, end="")
