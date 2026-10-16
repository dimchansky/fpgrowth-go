package isrout

// scanClass is upstream's scftab (util/src/scanner.c): the class of a byte
// in the scanable form of a name. 0: copied, no quotes needed; 1: copied,
// the name needs quotes; 2: written as \xhh; any other value c: written
// as the escape \c.
var scanClass = [256]byte{
	/* 00 */ 2, 2, 2, 2, 2, 2, 2, 'a',
	/* 08 */ 'b', 't', 'n', 'v', 'f', 'r', 2, 2,
	/* 10 */ 2, 2, 2, 2, 2, 2, 2, 2,
	/* 18 */ 2, 2, 2, 2, 2, 2, 2, 2,
	/* 20 */ 1, 1, '"', 1, 1, 1, 1, 1, // ' ' ! " # $ % & '
	/* 28 */ 1, 1, 1, 0, 1, 0, 0, 1, //   ( ) * + , - . /
	/* 30 */ 0, 0, 0, 0, 0, 0, 0, 0, //   0..7
	/* 38 */ 0, 0, 1, 1, 1, 1, 1, 1, //   8 9 : ; < = > ?
	/* 40 */ 1, 0, 0, 0, 0, 0, 0, 0, //   @ A..G
	/* 48 */ 0, 0, 0, 0, 0, 0, 0, 0,
	/* 50 */ 0, 0, 0, 0, 0, 0, 0, 0,
	/* 58 */ 0, 0, 0, 1, '\\', 1, 1, 0, // X Y Z [ \ ] ^ _
	/* 60 */ 1, 0, 0, 0, 0, 0, 0, 0, //   ` a..g
	/* 68 */ 0, 0, 0, 0, 0, 0, 0, 0,
	/* 70 */ 0, 0, 0, 0, 0, 0, 0, 0,
	/* 78 */ 0, 0, 0, 1, 1, 1, 1, 2, //   x y z { | } ~ DEL
	/* 80 */ 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	/* 90 */ 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	/* a0 */ 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	/* b0 */ 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	/* c0 */ 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	/* d0 */ 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	/* e0 */ 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	/* f0 */ 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
}

// scanable returns the name as isr_createx would store it for scanable
// output (option -g): unchanged if every byte is of class 0 (this includes
// the empty name, as scn_fmtlen reports no extra length for it), else in
// the quoted form of scn_format.
func scanable(name string) string {
	for i := range len(name) {
		if scanClass[name[i]] != 0 {
			return string(appendScanable(nil, name))
		}
	}
	return name
}

// appendScanable is a port of scn_format(dst, name, 0) for a name that
// needs quotes: the name in double quotes, with the bytes of class 2
// written as \xhh and those of the escape classes as \c.
func appendScanable(dst []byte, name string) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, '"')
	for i := range len(name) {
		c := name[i]
		switch cls := scanClass[c]; {
		case cls < 2:
			dst = append(dst, c)
		case cls > 2:
			dst = append(dst, '\\', cls)
		default:
			dst = append(dst, '\\', 'x', hex[c>>4], hex[c&0xf])
		}
	}
	return append(dst, '"')
}
