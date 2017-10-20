package govfs

import (
    "fmt"
)

func out(debug string) {
    fmt.Println(debug)
}

func out_hex(debug []byte) {
    fmt.Printf("%v\r\n", debug)
}
