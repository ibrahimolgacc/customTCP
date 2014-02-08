package main

import (
	//"github.com/pashap/myTCP"
	"github.com/pashap/myHTTP"
	"io/ioutil"
	"log"
	"net/http"
)

const (
	OriginHost string = "localhost"
	OriginPort string = ":1200"
)

func clientHandler(w http.ResponseWriter, r *http.Request) {
	getURL := r.URL.Path[1:]
	log.Printf("getURL: %s", getURL)

	file, err := ioutil.ReadFile("ProxyFiles/" + r.URL.Path[1:])
	if err != nil {
		originURL := myHTTP.MyURL{Host: OriginHost, Port: OriginPort, Path: r.URL.Path}
		html := myHTTP.Get(originURL)

		//save file in directory
		ioutil.WriteFile("ProxyFiles/"+r.URL.Path[1:], html, 0666) // 0666 = read permission

		w.WriteHeader(http.StatusOK)
		w.Write(html)
	} else {
		w.WriteHeader(http.StatusOK)
		w.Write(file)
	}
}

func main() {
	// Turn on some logging flags.
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime)

	http.HandleFunc("/", clientHandler) //registers handler func to root url

	log.Fatal(http.ListenAndServe(":8080", nil)) //nil means calls are dispatched to all registered handlers
}
