package calc

import (
	"bytes"
	"sort"
	"strconv"
	"time"
)

func writeDuration(b *bytes.Buffer, d time.Duration) {
	f := float64(d) / float64(time.Second)
	b.WriteString(strconv.FormatFloat(f, 'e', 10, 64))
}

func Format(b *bytes.Buffer, dt []time.Duration, count uint64) {
	b.WriteString("U:")

	var valid []time.Duration
	for _, d := range dt {
		if d != -1 {
			valid = append(valid, d)
		}
	}
	var countOk = len(valid)
	if countOk == 0 {
		b.WriteString(strconv.FormatUint(count, 10))
		b.WriteString(":U")
		for i := uint64(0); i < count; i++ {
			b.WriteString(":U")
		}

		return
	}

	sort.Slice(valid, func(i, j int) bool {
		return valid[i] < valid[j]
	})

	b.WriteString(strconv.FormatUint(count-uint64(countOk), 10))
	b.WriteByte(':')

	writeDuration(b, valid[countOk/2])
	//if countOk%1 == 0 {
	//	writeDuration(b, valid[countOk/2])
	//} else {
	//	writeDuration(b, (valid[countOk/2-1]+valid[countOk/2])/2)
	//}

	lost := count - uint64(countOk)
	for i := uint64(0); i < lost/2; i++ {
		b.WriteString(":U")
	}

	for _, d := range valid {
		b.WriteByte(':')
		writeDuration(b, d)
	}

	for i := uint64(0); i < (lost+1)/2; i++ {
		b.WriteString(":U")
	}
}
