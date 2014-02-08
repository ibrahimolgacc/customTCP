package main

import (
	"github.com/pashap/myHTTP"
	"github.com/pashap/myTCP"
	"log"
)

type HandleGet func(string) bool

func main() {
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime)
	var s myTCP.Server

	s.SetHandler(myHTTP.HandleGet)
	s.Listen(":1200")
}
