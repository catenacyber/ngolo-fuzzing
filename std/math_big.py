#python
import sys

# special patch as math/big.Float.Sqrt may panic with ErrNan
patch='''			case string, big.ErrNaN:'''

# avoid too big timeout exponentiation
patchExp='''                       if arg1.BitLen() + arg2.BitLen() > 1024 && arg3.BitLen() <= 1 {
                                continue
                        }'''

f = open(sys.argv[1])
for l in f.readlines():
    if "case string:" in l:
        print(patch)
    elif "r0 := arg0.Exp(arg1, arg2, arg3)" in l:
        print(patchExp)
        print(l, end="")
    else:
        print(l, end="")
