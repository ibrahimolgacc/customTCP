package myHTTP

import (
	"github.com/pashap/myTCP"
	"log"
	"io/ioutil"
	"strconv"
	"strings"
)

type MyURL struct {
	Host string
	Port string
	Path string
}

type Request struct {
	Method        string // GET, POST, PUT, etc
	Path          string
	Host          string
	Version       string
	ContentLength int64
	Body          string
}

type Response struct {
	Version       string
	Status        string
	ContentLength int64
}


//							(file, err)
func HandleGet(msg []byte) ([]byte, int) {
	//parses msg to see if it is correct message
	expectedString := "GET /webPage.html HTTP/1.1"+"\n"+"Host: localhost Content-Length: 0\n\n"
	if strings.EqualFold(string(msg[:]), expectedString) {
		//log.Println("Likes what it sees: ", string(msg[5:17]))
		file, err := ioutil.ReadFile("OriginFiles/"+string(msg[5:17]))
		if err != nil {
			file, _ = ioutil.ReadFile("OriginFiles/notFound.html")
		}		
		return file, 0
	}

	return nil, 1 //tells transport to keep reading message	
}

// orders the fields to a proper HTTP request => http://www.w3.org/Protocols/rfc2616/rfc2616-sec5.html#sec5
func (req *Request) ToString() string {
	s := req.Method + " " + req.Path + " " + req.Version + "\n" + "Host: " + req.Host + " " + "Content-Length: " + strconv.FormatInt(req.ContentLength, 10) + "\n\n" + req.Body
	return s
}

func Get(url MyURL) []byte {

	// establish a tcp connection with server		
	myTCPconn := myTCP.Dial(url.Host + url.Port)

	//create http request
	//length := len("get me that page fool")
	length := len("")
	//log.Println("URL PATH: ", url.Path)
	req := Request{Method: "GET", Path: url.Path, Host: url.Host, Version: "HTTP/1.1", ContentLength: int64(length)}
	log.Println("Request: ", req)

	r := req.ToString()
	
	// Handles the RDT
	myTCPconn.Write(r)

	//read response from server
	// if HTTP Response fields correct:
	// body should be the file requested
	var buf []byte
	buf = myTCPconn.Read() // myTCPconn has an internal buffer to save the servers response
	
	//close tcp connection
	myTCPconn.Close()

	//return requested file
	return buf
}


