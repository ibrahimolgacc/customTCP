customTCP
=========

### Summary
Project for a grad level network course at MST. <br>
Most of code is in myTCP. <br>
Both the server and client run a custom reliable data transfer protocol to exchange app-layer files. <br>
It was a public lab project, no access rights to change the protocol stack in the OS:

> Everything for the protocol is done above the socket API(using UDP).

<br>
### Protocol
The service must detect/correct packet loss.
- Retransmits 

<br>
* Request a web page from a 

<br>
### To Execute
To run the Origin Server from command line: 
> go run OriginServer.go 

<br>
To run the Proxy Server from command line: 
> go run ProxyServer.go

<br>
Then on a web browser:
> go to address localhost:8080/webPage.html 

<br>
The requested page will appear and also is saved in the ProxyFiles directory.
