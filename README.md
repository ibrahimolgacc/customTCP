customTCP
=========

# The largest heading (an <h1> tag)
## The second largest heading (an <h2> tag)
…
###### The 6th largest heading (an <h6> tag)


Project for a grad level network course at MST.
Both the server and client run a custom reliable data transfer protocol to exchange app-layer files.
Everything for the protocol is done above the socket API(using UDP).
- public lab project, no access rights to change the protocol stack in the OS
The service must detect/retransmit packet loss.
    -


- Request a web page from a 



To run the Origin Server from command line: go run OriginServer.go
To run the Proxy Server from command line: go run ProxyServer.go
Then on a web browser, go to address localhost:8080/webPage.html
The requested page will appear and also is saved in the ProxyFiles directory.
