package duggy

type Processor struct {
	options uint32
}

func CreateProcessor(options uint32) *Processor {
	r := &Processor{}
	r.options = options
	return r
}

func Process(p *Processor, data []byte) int {
	switch p.options {
	case 0:
		return 0
	case 42:
		if len(data) < 3 {
			return -1
		}
		if data[0] == 'B' {
			if data[1] == 'U' {
				if data[2] == 'G' {
					panic("Duggy bug !")
				}
			}
		}
	}
	return 1
}
