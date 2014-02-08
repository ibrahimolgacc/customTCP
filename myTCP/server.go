package myTCP

import (
	"container/list"
	"log"
	"math"
	"math/rand"
	"net"
	"time"
)

const (
	//States for server
	Listen            = iota //6       
	SynReceived       = iota //7	
	ServerEstablished = iota //8	
	CloseWait         = iota //9	
	LastAck           = iota //10
)

/* Server Side Functionality */

// For HTTP Layer => Server side functions should have this signature
type HandlerFn func([]byte) ([]byte, int)

type Server struct {
	UdpConn   *net.UDPConn
	clients   map[string]TCP_Client
	handlerFn HandlerFn
}

type TCP_Client struct {
	connection          *net.UDPConn
	cAddr               *net.UDPAddr
	state               uint16
	rcv_seg_chan        chan TCP_Segment
	server_rcv_seg_chan chan TCP_Segment
	send_seg_chan       chan TCP_Segment
	app_chan            chan []byte
	finished_chan       chan int
	timeout_chan        <-chan time.Time
	clientPort          uint16
	serverPort          uint16
	clientSequence      uint32 // this is the clients
	serverSequence      uint32 // this is the servers
	rcv_buffer          []byte
	buf_pos             int
	REQUESTED           []byte
	num_acks            map[uint32]int
	already_in_buf      map[uint32]bool
	last_seg_sent       TCP_Segment
}

// Communicates with App-layer via s.app_chan
// Blocks until it receives whats in the buffer
func (s *Server) SetHandler(fn HandlerFn) {
	s.handlerFn = fn
}

func (s *Server) Listen(port string) {
	//random generator for the sequence numbers
	rand.Seed(time.Now().UTC().UnixNano())

	udpAddr, err := net.ResolveUDPAddr("udp4", "localhost"+port)
	checkError(err)

	UdpConn, _ := net.ListenUDP("udp", udpAddr)
	log.Println("Server addr: ", udpAddr)
	s.UdpConn = UdpConn
	checkError(err)

	///////////////// Receive Packets ///////////////// 
	s.clients = make(map[string]TCP_Client)
	var buf [UDP_BUF_LEN]byte
	for {

		log.Println("Waiting for segments")
		n, addr, err := s.UdpConn.ReadFromUDP(buf[0:])
		if err == nil {
			segmentReceived := Deserialize(buf, n)
			//log.Println("N : ", n)
			log.Println("Server received segment: ", segmentReceived.Header)
			cPort := segmentReceived.Header.SrcPort
			sPort := segmentReceived.Header.DstPort
			seq := segmentReceived.Header.Seq
			flags := segmentReceived.Header.Flags

			connectionKey := addr.String() + string(cPort) + string(sPort) // "IP-SOURCEPORT-DESTPORT" is key
			//check if this connection exists
			currentClient, connectionExists := s.clients[connectionKey]
			if connectionExists {
				//go curClient.manageClient() is already running in the background, Send it TCP_SEGMENT
				currentClient.server_rcv_seg_chan <- segmentReceived //WILL BLOCK IF LAST SEG NOT READ YET!!
			} else {
				//check that SYN bit is set to 1, if not, then client is not using TCP			
				if flags == SYN {
					//create client-> initialize buffer, state variables, and channel to communicate with
					clientRcvChan := make(chan TCP_Segment)
					clientSendChan := make(chan TCP_Segment)
					serverRcvChan := make(chan TCP_Segment)
					appChan := make(chan []byte)
					finishedChan := make(chan int)
					n_acks := make(map[uint32]int)
					alr_in_buf := make(map[uint32]bool)
					rcv_buf := make([]byte, 0)
					client := TCP_Client{connection: s.UdpConn, cAddr: addr, state: Listen, rcv_seg_chan: clientRcvChan, rcv_buffer: rcv_buf, buf_pos: 0, already_in_buf: alr_in_buf,
						send_seg_chan: clientSendChan, clientPort: cPort, server_rcv_seg_chan: serverRcvChan, finished_chan: finishedChan,
						serverPort: sPort, clientSequence: seq, serverSequence: uint32(rand.Intn(65535)), app_chan: appChan, num_acks: n_acks}
					s.clients[connectionKey] = client

					go client.manageClient()
					go client.handleApp(s.handlerFn)
				}
			}
		}
	}
}

func (client *TCP_Client) manageClient() {
	go client.rdt()

	///////////////// Send Packets /////////////////	
	go func(client *TCP_Client) {
		var seg TCP_Segment
		var dataEmpty [MSS]byte
		for {
			seg = <-client.send_seg_chan

			if segmentDebug {
				if seg.Data != dataEmpty {
					log.Println("Sent Segment: ", seg)
				} else {
					log.Println("Sent Segment: ", seg.Header)
				}
			}
			if seg.Header.Flags != ACK {
				client.timeout_chan = time.After(3 * time.Second)
				log.Println("Set Timer on Send")
			}

			client.last_seg_sent = seg
			packet := seg.Serialize()

			myNetwork_serverSend(client.connection, packet, client.cAddr)
		}
	}(client)
	///////////////// Send Packets /////////////////
	///////////////// Receive Packets ///////////////// 
	go func(client *TCP_Client) {
		var seg TCP_Segment
		var dataEmpty [MSS]byte
		for {
			log.Println("Receive")
			select {
			case <-client.timeout_chan:

				log.Println("Timeout for Segment: ", client.last_seg_sent.Header)
				client.send_seg_chan <- client.last_seg_sent

			case seg = <-client.server_rcv_seg_chan:

				if seg.Header.Flags == ACK {
					log.Println("Trying to stop timer")
					<-client.timeout_chan // STOP TIMER ??????
					log.Println("Timer stopped")
				}

				if segmentDebug {
					if seg.Data != dataEmpty {
						log.Println("Received Segment: ", seg)
					} else {
						log.Println("Received Segment: ", seg.Header)
					}
				}

				client.rcv_seg_chan <- seg
			}
		}
	}(client)
	///////////////// Receive Packets ///////////////// 

	<-client.finished_chan
}

// manageClient is the servers FSM for handling each client connection
// STATES -> 
//			Listen(rec SYN, send SYNACK)
//			SynReceived(rec ACK, send nothing)
//			Established if(rec FIN, send ACK) 
//			CloseWait(send FIN)
//			LastAck(rec ACK, send nothing)	
func (client *TCP_Client) rdt() {
	var dataEmpty [MSS]byte
	var synack_seg TCP_Segment
	var seg_rcvd TCP_Segment
	var ack_seg TCP_Segment
	var send_seg TCP_Segment
	var fin_seg TCP_Segment
	var send_msg_len int
	var segmentList *list.List
	var current_send *list.Element

	for {
		switch client.state {
		case Listen:

			client.clientSequence = client.clientSequence + 1 //clients syn consumed 1 seq
			//Create SYNACK packet
			synack_seg = TCP_Segment{
				Header: TCP_Header{SrcPort: client.serverPort, DstPort: client.clientPort, Seq: client.serverSequence,
					Ack: client.clientSequence, Flags: SYNACK, WindowSize: WIN_SIZE},
				Data: dataEmpty}

			client.send_seg_chan <- synack_seg //timer is set on server side if dropped
			log.Println("HS2, segment sent: ", synack_seg.Header)

			//Move client to the next state
			client.state = SynReceived

		case SynReceived:

			seg_rcvd = <-client.rcv_seg_chan
			log.Println("HS3, received: ", seg_rcvd.Header)

			if seg_rcvd.Data == dataEmpty && seg_rcvd.Header.Flags == ACK {
				//check that client has acked the correct server seq
				if seg_rcvd.Header.Ack == client.serverSequence+1 {
					// Increment server sequence
					client.serverSequence = client.serverSequence + 1
					if segmentDebug {
						log.Println("Ack for SYNACK received")
					}

					client.state = ServerEstablished
				}
			} else if seg_rcvd.Data == dataEmpty && seg_rcvd.Header.Flags == SYN {
				client.send_seg_chan <- synack_seg

			} else {
				//resend last ack
				client.send_seg_chan <- client.last_seg_sent
			}

		case ServerEstablished:
			//Wait for message

			seg_rcvd = <-client.rcv_seg_chan

			if seg_rcvd.Header.Flags == FIN { // if the segment has its FIN flag set, we move to CloseWait
				//need to ack this fin
				if segmentDebug {
					log.Println("Established->Server received FIN segment")
				}

				//FIN consumes one seq num
				client.clientSequence = client.clientSequence + 1

				ack_seg = TCP_Segment{
					Header: TCP_Header{SrcPort: client.serverPort, DstPort: client.clientPort, Seq: client.serverSequence,
						Ack: client.clientSequence, Flags: ACK, WindowSize: WIN_SIZE},
					Data: dataEmpty}

				client.send_seg_chan <- ack_seg

				//ACK alone consumes no seq num			
				client.state = CloseWait

				//client.close_wait_chan <- seg_rcvd //let the case CloseWait below handle all logic
			} else {
				if segmentDebug {
					log.Println("Established")
				}

				msg_len := segment_data_len(seg_rcvd.Data)
				client.clientSequence = client.clientSequence + uint32(msg_len)

				if client.REQUESTED != nil { //RESPOND
					log.Println("RESPOND")
					//log.Println("File", string(client.REQUESTED))

					//check that flags are ACK
					if seg_rcvd.Header.Flags == ACK {
						//check that acknowledgment is correct
						if seg_rcvd.Header.Ack == client.serverSequence {
							current_send = current_send.Next()
							//update client buffer window size

							if current_send != nil { //Still more segments to send as response
								send_seg = current_send.Value.(TCP_Segment)
								//increment server sequence in order to ack the correct segment
								send_msg_len = segment_data_len(send_seg.Data)
								client.serverSequence = client.serverSequence + uint32(send_msg_len)
								client.send_seg_chan <- send_seg
							} else { //No more segments in response, reset client.REQUESTED to nil, back to request mode
								client.REQUESTED = nil
							}
						}
					}

				} else if msg_len != 0 { //REQUEST
					log.Println("REQUEST")

					// ACK the packet
					ack_seg = TCP_Segment{
						Header: TCP_Header{SrcPort: client.serverPort, DstPort: client.clientPort, Seq: client.serverSequence,
							Ack: client.clientSequence, Flags: ACK, WindowSize: WIN_SIZE},
						Data: dataEmpty}

					client.send_seg_chan <- ack_seg

					//check if this message segment is already in the buffer
					_, inBuffer := client.already_in_buf[seg_rcvd.Header.Seq]
					if !inBuffer {
						client.rcv_buffer = append(client.rcv_buffer, seg_rcvd.Data[0:msg_len]...)
						client.already_in_buf[seg_rcvd.Header.Seq] = true
					}

					client.app_chan <- client.rcv_buffer[:]

					file := <-client.app_chan
					if file != nil { //message was received correctly
						client.REQUESTED = file
						segmentList = client.segmentResponse(string(client.REQUESTED[:]))
						current_send = segmentList.Front()
						send_seg = current_send.Value.(TCP_Segment)
						//increment server sequence in order to ack the correct segment
						send_msg_len = segment_data_len(send_seg.Data)
						client.serverSequence = client.serverSequence + uint32(send_msg_len)
						client.send_seg_chan <- send_seg
					}

				}

			}

		case CloseWait:
			if segmentDebug {
				log.Println("CloseWait")
			}

			select {
			case seg_rcvd = <-client.rcv_seg_chan:
				log.Println("Retransmit")
				client.send_seg_chan <- client.last_seg_sent //retransmit last ack
			default:
				log.Println("Sending Ack for client FIN")
				//FIN consumes one seq num
				client.serverSequence = client.serverSequence + 1
				// Send FIN
				fin_seg = TCP_Segment{Header: TCP_Header{SrcPort: client.serverPort, DstPort: client.clientPort,
					Seq: client.serverSequence, Ack: client.clientSequence, Flags: FIN, WindowSize: WIN_SIZE},
					Data: dataEmpty}

				//Send packet
				client.send_seg_chan <- fin_seg
				client.state = LastAck
				break
			}

		case LastAck:
			if segmentDebug {
				log.Println("LastAck")
			}

			//rec ack and send nothing
			// if correct ack, close connection
			ack_seg = <-client.rcv_seg_chan
			log.Println("Received last ack: ", ack_seg.Header)
			if ack_seg.Header.Ack == client.serverSequence {
				log.Println("Sent Finished on channel")
				client.finished_chan <- 1
				break
			}

		}
	}
}

func (client *TCP_Client) segmentResponse(data string) *list.List {
	msgBytes := []byte(data)
	l := len(data)
	numSegments := int(math.Ceil(float64(l) / float64(MSS)))
	remainder := l % MSS

	segmentList := list.New()

	// segment(split up) the message and create TCP_Segment(s) for it
	var segment TCP_Segment
	var emptySegment TCP_Segment

	// DONT ASSIGN SEQUENCE NUMBER HERE, just use a local variable
	server_sequence := client.serverSequence

	if numSegments == 1 {
		segment = emptySegment
		segment.Header.SrcPort = client.serverPort
		segment.Header.DstPort = client.clientPort

		segment.Header.Seq = client.serverSequence
		segment.Header.Ack = client.clientSequence
		segment.Header.Flags = NONE
		segment.Header.WindowSize = WIN_SIZE

		copy(segment.Data[0:l], msgBytes[0:l])
		//client.serverSequence = client.serverSequence + uint32(l)
		server_sequence = server_sequence + uint32(l)
		segmentList.PushBack(segment)
	} else {
		for i := 0; i < int(numSegments); i++ {
			segment = emptySegment
			segment.Header.SrcPort = client.serverPort
			segment.Header.DstPort = client.clientPort

			segment.Header.Seq = client.serverSequence
			segment.Header.Ack = client.clientSequence
			segment.Header.Flags = NONE
			segment.Header.WindowSize = WIN_SIZE

			if i != int(numSegments)-1 {
				copy(segment.Data[0:MSS], msgBytes[i*MSS:i*MSS+MSS])
				//Segment contains MSS bytes => We need to increment clientSeq by MSS
				//client.serverSequence = client.serverSequence + uint32(MSS)
				server_sequence = server_sequence + uint32(MSS)
				segmentList.PushBack(segment)

			} else {
				//log.Println(msgBytes[i*MSS : i*MSS+remainder])
				copy(segment.Data[0:MSS], msgBytes[i*MSS:i*MSS+remainder])
				//Segment contains remainder bytes => We need to increment clientSeq by remainder
				//client.serverSequence = client.serverSequence + uint32(remainder)
				server_sequence = server_sequence + uint32(remainder)
				segmentList.PushBack(segment)

			}
		}
	}

	return segmentList
}

func myNetwork_serverSend(c *net.UDPConn, packet []byte, cAddr *net.UDPAddr) {
	if rand.Intn(4) != 10 { //25% failure rate
		c.WriteToUDP(packet, cAddr)
	} else {
		log.Println("Dropped Packet: ", packet)
	}

}

func (client *TCP_Client) handleApp(fn HandlerFn) {
	for {
		var currentRcvBuffer []byte
		currentRcvBuffer = <-client.app_chan
		file, err := fn(currentRcvBuffer)
		if err != 0 {
			client.app_chan <- nil
		} else {
			client.app_chan <- file //either what was requested or notFound.html
		}
	}
}
