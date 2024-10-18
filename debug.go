package main

import (
	"fmt"
	"runtime"
)

const (
	debugNone            = 0
	debugIO              = 1
	debugAddFunctionName = 15
)

//const DebugLevel uint32 = debugIO | (1<<debugAddFunctionName - 1)
const DebugLevel uint32 = debugNone

func debugIOPrintf(format string, a ...interface{}) (int, error) {
	var s string
	var n int
	var err error

	if DebugLevel&(1<<(debugIO-1)) != 0 {
		if DebugLevel&(1<<(debugAddFunctionName-1)) != 0 {
			pc, _, _, ok := runtime.Caller(1)
			s = "?"
			if ok {
				fn := runtime.FuncForPC(pc)
				if fn != nil {
					s = fn.Name()
				}
			}
			newformat := "[" + s + "] " + format
			n, err = fmt.Printf(newformat, a...)
		} else {
			n, err = fmt.Printf(format, a...)
		}
		return n, err
	}
	return 0, nil
}
