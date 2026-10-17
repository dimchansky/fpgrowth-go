package main

import (
	"fmt"
	"os"
)

func main() {
	fi, err := os.Stdout.Stat()
	_, werr := os.Stdout.Write([]byte("x\n"))
	if fi != nil {
		fmt.Fprintln(os.Stderr, "stat:", fi.Name(), fi.Mode(), err, "write err:", werr)
	} else {
		fmt.Fprintln(os.Stderr, "stat err:", err, "write err:", werr)
	}
}
