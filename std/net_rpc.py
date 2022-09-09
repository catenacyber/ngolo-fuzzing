#python
import sys

# special patch as rpc.Register to avoid pass a nil object
patch='''			if a.Register.Rcvr == nil {
				return 0
			}'''

f = open(sys.argv[1])
patched = 0
for l in f.readlines():
    if "rpc.Register(a.Register.Rcvr)" in l and patched < 1:
        print(patch)
        patched = patched + 1
    print(l, end="")
