#python
import sys

# special patch as math/big.Float.Sqrt may panic with ErrNan
patch='''			case string, big.ErrNaN:'''

f = open(sys.argv[1])
for l in f.readlines():
    if "case string:" in l:
        print(patch)
    else:
        print(l, end="")
