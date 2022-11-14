#python
import sys

# special patch as ascii85.Encode needs some length
patch='''			a.Encode.Dst = make([]byte, MaxEncodedLen(len(a.Encode.Src)))'''

f = open(sys.argv[1])
patched = False
for l in f.readlines():
    if "a.Encode.Dst = make([]byte, 2*len(a.Encode.Src))" in l and not patched:
        print(patch)
        patched = True
        continue
    print(l, end="")
